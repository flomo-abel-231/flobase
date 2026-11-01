package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Docker runs each project as its own containers, network and volumes on a
// Docker host, talking to the Engine API over its unix socket.
type Docker struct {
	http       *http.Client
	publicHost string   // host clients use to reach published ports
	attach     []string // containers joined to every project network (control plane, gateway)
	offsiteMu  sync.RWMutex
	offsite    *Offsite // nil = backups stay on this machine only; see SetOffsite
	// relay is set for a region server: project services are reached
	// through it instead of by joining their networks (see relay.go).
	relay   *Relay
	hostKey func() string
	// ca issues projects' database certificates; nil = no TLS.
	ca *CA
}

func NewDocker(socket, publicHost string, attach []string, offsite *Offsite) *Docker {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}
	return &Docker{http: &http.Client{Transport: transport}, publicHost: publicHost, attach: attach, offsite: offsite}
}

// SetOffsite changes where off-site copies go (nil = nowhere). Projects'
// sync containers pick it up when they are restarted; see the api package.
func (d *Docker) SetOffsite(o *Offsite) {
	d.offsiteMu.Lock()
	d.offsite = o
	d.offsiteMu.Unlock()
}

// currentOffsite is the storage in use now. An Offsite is never changed in
// place, so the pointer is safe to use after the lock is released.
func (d *Docker) currentOffsite() *Offsite {
	d.offsiteMu.RLock()
	defer d.offsiteMu.RUnlock()
	return d.offsite
}

const label = "dev.flobase.project"

// container is one container of a project.
type container struct {
	suffix      string
	image       string
	user        string // empty = the image's default
	entrypoint  []string
	cmd         []string
	env         []string
	port        int // container port published on the host; 0 = not published
	hostPort    int // host port to publish on; 0 = Docker picks a free one
	mounts      []mount
	healthcheck []string
	// oneShot containers run a setup script to completion and are removed
	// before the next container starts. everyStart ones also run when the
	// project is recreated.
	oneShot    bool
	everyStart bool
	// ready runs once the container is healthy, with the address the
	// control plane reaches it on.
	ready func(ctx context.Context, host string, port int) error
}

// mount is a named volume. Volumes named after the project are created with
// its label, so deleting the project removes them; read-only mounts borrow
// another project's volume, for example its backups during a restore.
type mount struct {
	volume   string
	path     string
	readOnly bool
}

// vol names one of a project's volumes.
func vol(ref, name string) string { return "flobase-" + ref + "-" + name }

// containers describes each engine as the containers it needs, in start
// order, and which one clients connect to.
func containers(s Spec) ([]container, string, error) {
	switch s.Engine {
	case "postgres":
		return postgresContainers(s), "db", nil
	case "mysql":
		return []container{{
			suffix: "db",
			image:  "mysql:8.4",
			env: []string{
				"MYSQL_ROOT_PASSWORD=" + s.Password, "MYSQL_DATABASE=" + s.Database,
				"MYSQL_USER=" + s.User, "MYSQL_PASSWORD=" + s.Password,
			},
			port:   3306,
			mounts: []mount{{volume: vol(s.Ref, "db-data"), path: "/var/lib/mysql"}},
			// Ping over TCP: during first start MySQL runs a temporary
			// socket-only server that a socket ping would report as ready.
			healthcheck: []string{"CMD-SHELL", "mysqladmin ping -h 127.0.0.1 -u" + s.User + " -p" + s.Password + " --silent"},
		}}, "db", nil
	case "mongodb":
		// FerretDB speaks the MongoDB wire protocol and stores documents in
		// Postgres with the DocumentDB extension; both stay open source.
		return append(mongoTLS(s), []container{
			{
				suffix: "pg",
				image:  "ghcr.io/ferretdb/postgres-documentdb:17",
				env: []string{
					"POSTGRES_USER=" + s.User, "POSTGRES_PASSWORD=" + s.Password, "POSTGRES_DB=postgres",
				},
				mounts:      []mount{{volume: vol(s.Ref, "pg-data"), path: "/var/lib/postgresql/data"}},
				healthcheck: []string{"CMD-SHELL", "pg_isready -U " + s.User + " -d postgres"},
			},
			{
				suffix: "db",
				image:  "ghcr.io/ferretdb/ferretdb:2",
				env: append([]string{
					"FERRETDB_POSTGRESQL_URL=postgres://" + s.User + ":" + s.Password + "@flobase-" + s.Ref + "-pg:5432/postgres",
				}, ferretTLSEnv(s)...),
				port:   27017,
				mounts: tlsMounts(s),
			},
		}...), "db", nil
	}
	return nil, "", fmt.Errorf("unknown engine %q", s.Engine)
}

func (d *Docker) Create(ctx context.Context, s Spec) (Endpoint, error) {
	s, err := d.withTLS(s)
	if err != nil {
		return Endpoint{}, err
	}
	specs, entry, err := containers(s)
	if err != nil {
		return Endpoint{}, err
	}
	if s.Restore != nil && s.Restore.Offsite {
		if d.currentOffsite() == nil {
			return Endpoint{}, fmt.Errorf("off-site storage is not set up")
		}
		specs = append([]container{d.fetchContainer(s)}, specs...)
	}
	network := "flobase-" + s.Ref
	if err := d.call(ctx, "POST", "/networks/create", map[string]any{
		"Name": network, "Labels": map[string]string{label: s.Ref},
	}, nil); err != nil {
		return Endpoint{}, fmt.Errorf("create network: %w", err)
	}
	if err := d.Attach(ctx, s.Ref); err != nil {
		return Endpoint{}, err
	}
	var ep Endpoint
	for _, c := range specs {
		if err := d.pull(ctx, c.image); err != nil {
			return Endpoint{}, fmt.Errorf("pull %s: %w", c.image, err)
		}
		name := "flobase-" + s.Ref + "-" + c.suffix
		if err := d.run(ctx, s, c, name, network); err != nil {
			return Endpoint{}, fmt.Errorf("start %s: %w", name, err)
		}
		if c.oneShot {
			if _, err := d.waitExit(ctx, name); err != nil {
				return Endpoint{}, err
			}
			continue
		}
		if c.healthcheck != nil {
			if err := d.waitHealthy(ctx, name); err != nil {
				return Endpoint{}, err
			}
		}
		published := 0
		if c.port != 0 {
			if published, err = d.hostPort(ctx, name, c.port); err != nil {
				return Endpoint{}, err
			}
		}
		if c.ready != nil {
			host, port := d.Addr(s.Ref, c.suffix, c.port, published)
			if err := c.ready(ctx, host, port); err != nil {
				return Endpoint{}, err
			}
		}
		switch {
		case c.suffix == entry:
			ep.Port = published
		case c.suffix == "pooler":
			ep.PoolerPort = published
		}
	}
	ep.Host = d.publicHost
	ep.TLS = s.TLSCert != "" || s.Engine == "mysql"
	if s.Restore != nil && s.Restore.Offsite {
		// The downloaded copy is no longer needed once the database is up.
		_ = d.call(ctx, "DELETE", "/volumes/"+vol(s.Ref, "incoming"), nil, nil)
	}
	return ep, nil
}

// Attach joins the containers named in attach to a project's network, so
// they reach its services by name. Already joined is not an error.
func (d *Docker) Attach(ctx context.Context, ref string) error {
	for _, name := range d.attach {
		err := d.call(ctx, "POST", "/networks/flobase-"+ref+"/connect", map[string]any{"Container": name}, nil)
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("attach %s to project network: %w", name, err)
		}
	}
	return nil
}

func (d *Docker) Addr(ref, service string, containerPort, publishedPort int) (string, int) {
	if d.relay != nil {
		host, port, err := d.relay.Forward("flobase-" + ref + "-" + service + ":" + strconv.Itoa(containerPort))
		if err != nil {
			return "127.0.0.1", 0
		}
		return host, port
	}
	if len(d.attach) > 0 {
		return "flobase-" + ref + "-" + service, containerPort
	}
	return d.publicHost, publishedPort
}

func (d *Docker) run(ctx context.Context, s Spec, c container, name, network string) error {
	restart := "unless-stopped"
	if c.oneShot {
		restart = "no"
	}
	host := map[string]any{
		"NetworkMode":   network,
		"RestartPolicy": map[string]any{"Name": restart},
	}
	// Plan limits apply to the database; services get a fixed small share.
	if c.suffix == "db" || c.suffix == "pg" {
		host["Memory"] = int64(s.Plan.MemoryMB) * 1024 * 1024
		host["NanoCpus"] = int64(s.Plan.CPU * 1e9)
	} else {
		host["Memory"] = int64(256) * 1024 * 1024
	}
	var mounts []map[string]any
	for _, m := range c.mounts {
		spec := map[string]any{"Type": "volume", "Source": m.volume, "Target": m.path, "ReadOnly": m.readOnly}
		if !m.readOnly {
			spec["VolumeOptions"] = map[string]any{"Labels": map[string]string{label: s.Ref}}
		}
		mounts = append(mounts, spec)
	}
	if mounts != nil {
		host["Mounts"] = mounts
	}
	config := map[string]any{
		"Image":      c.image,
		"Env":        c.env,
		"Labels":     map[string]string{label: s.Ref},
		"HostConfig": host,
	}
	// Aliases exist only on user-defined networks, not the default bridge.
	if network != "bridge" {
		config["NetworkingConfig"] = map[string]any{
			"EndpointsConfig": map[string]any{network: map[string]any{"Aliases": []string{name}}},
		}
	}
	if c.entrypoint != nil {
		config["Entrypoint"] = c.entrypoint
	}
	if c.cmd != nil {
		config["Cmd"] = c.cmd
	}
	if c.user != "" {
		config["User"] = c.user
	}
	if c.port != 0 {
		port := strconv.Itoa(c.port) + "/tcp"
		config["ExposedPorts"] = map[string]any{port: map[string]any{}}
		// An empty HostPort lets Docker pick a free port.
		hostPort := ""
		if c.hostPort != 0 {
			hostPort = strconv.Itoa(c.hostPort)
		}
		host["PortBindings"] = map[string]any{port: []map[string]string{{"HostPort": hostPort}}}
	}
	if c.healthcheck != nil {
		config["Healthcheck"] = map[string]any{
			"Test": c.healthcheck, "Interval": int64(2 * time.Second), "Timeout": int64(5 * time.Second),
			"Retries": 60, "StartPeriod": int64(5 * time.Second),
		}
	}
	if err := d.call(ctx, "POST", "/containers/create?name="+url.QueryEscape(name), config, nil); err != nil {
		return err
	}
	return d.call(ctx, "POST", "/containers/"+name+"/start", nil, nil)
}

type inspect struct {
	State struct {
		Status   string
		ExitCode int
		Health   *struct{ Status string }
	}
	NetworkSettings struct {
		Ports map[string][]struct{ HostPort string }
	}
}

func (d *Docker) inspect(ctx context.Context, name string) (inspect, error) {
	var out inspect
	err := d.call(ctx, "GET", "/containers/"+name+"/json", nil, &out)
	return out, err
}

func (d *Docker) waitHealthy(ctx context.Context, name string) error {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		info, err := d.inspect(ctx, name)
		if err != nil {
			return err
		}
		if info.State.Status == "exited" || info.State.Status == "dead" {
			return fmt.Errorf("%s stopped while starting", name)
		}
		if info.State.Health != nil && info.State.Health.Status == "healthy" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("%s did not become healthy within 3 minutes", name)
}

// waitExit waits for a one-shot container, removes it, and returns its
// output; a failure carries the last lines.
func (d *Docker) waitExit(ctx context.Context, name string) (string, error) {
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		info, err := d.inspect(ctx, name)
		if err != nil {
			return "", err
		}
		if info.State.Status == "exited" || info.State.Status == "dead" {
			out, _ := d.containerLogs(ctx, name, 0, false)
			_ = d.call(ctx, "DELETE", "/containers/"+name+"?force=true", nil, nil)
			if info.State.ExitCode != 0 {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				return out, fmt.Errorf("%s failed: %s", name, strings.Join(lines[max(0, len(lines)-15):], "\n"))
			}
			return out, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("%s did not finish within 20 minutes", name)
}

func (d *Docker) hostPort(ctx context.Context, name string, port int) (int, error) {
	info, err := d.inspect(ctx, name)
	if err != nil {
		return 0, err
	}
	for _, b := range info.NetworkSettings.Ports[strconv.Itoa(port)+"/tcp"] {
		if p, err := strconv.Atoi(b.HostPort); err == nil && p > 0 {
			return p, nil
		}
	}
	return 0, fmt.Errorf("%s has no published port for %d", name, port)
}

// pull fetches an image unless it is already present.
func (d *Docker) pull(ctx context.Context, image string) error {
	if d.call(ctx, "GET", "/images/"+image+"/json", nil, nil) == nil {
		return nil
	}
	ref, tag := image, "latest"
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		ref, tag = image[:i], image[i+1:]
	}
	req, err := http.NewRequestWithContext(ctx, "POST",
		"http://docker/images/create?fromImage="+url.QueryEscape(ref)+"&tag="+url.QueryEscape(tag), nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker: %s", strings.TrimSpace(string(body)))
	}
	// The pull streams progress as JSON lines; errors arrive in the stream.
	dec := json.NewDecoder(resp.Body)
	for {
		var msg struct{ Error string }
		if err := dec.Decode(&msg); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if msg.Error != "" {
			return errors.New(msg.Error)
		}
	}
}

func (d *Docker) Delete(ctx context.Context, ref string) error {
	filter := url.QueryEscape(`{"label":["` + label + `=` + ref + `"]}`)
	var list []struct{ Id string }
	if err := d.call(ctx, "GET", "/containers/json?all=true&filters="+filter, nil, &list); err != nil {
		return err
	}
	for _, c := range list {
		if err := d.call(ctx, "DELETE", "/containers/"+c.Id+"?force=true", nil, nil); err != nil && !isNotFound(err) {
			return fmt.Errorf("remove container: %w", err)
		}
	}
	var volumes struct{ Volumes []struct{ Name string } }
	if err := d.call(ctx, "GET", "/volumes?filters="+filter, nil, &volumes); err != nil {
		return err
	}
	for _, v := range volumes.Volumes {
		if err := d.call(ctx, "DELETE", "/volumes/"+v.Name, nil, nil); err != nil && !isNotFound(err) {
			return fmt.Errorf("remove volume: %w", err)
		}
	}
	for _, name := range d.attach {
		_ = d.call(ctx, "POST", "/networks/flobase-"+ref+"/disconnect", map[string]any{"Container": name, "Force": true}, nil)
	}
	if err := d.call(ctx, "DELETE", "/networks/flobase-"+ref, nil, nil); err != nil && !isNotFound(err) {
		return fmt.Errorf("remove network: %w", err)
	}
	return nil
}

// Logs returns the last lines of a service's output, stdout and stderr
// interleaved.
func (d *Docker) Logs(ctx context.Context, ref, service string, lines int) (string, error) {
	return d.containerLogs(ctx, "flobase-"+ref+"-"+service, lines, true)
}

// containerLogs reads a container's output; lines 0 = all of it.
func (d *Docker) containerLogs(ctx context.Context, name string, lines int, timestamps bool) (string, error) {
	tail := "all"
	if lines > 0 {
		tail = strconv.Itoa(lines)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf(
		"http://docker/containers/%s/logs?stdout=true&stderr=true&timestamps=%t&tail=%s", name, timestamps, tail), nil)
	if err != nil {
		return "", err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", &apiError{status: resp.StatusCode, message: "no such service"}
	} else if resp.StatusCode >= 300 {
		return "", &apiError{status: resp.StatusCode, message: strings.TrimSpace(string(raw))}
	}
	return demux(raw), nil
}

// demux joins Docker's framed output: without a TTY each chunk has an
// 8-byte header whose last 4 bytes are the chunk length.
func demux(raw []byte) string {
	var out strings.Builder
	for len(raw) >= 8 {
		size := int(raw[4])<<24 | int(raw[5])<<16 | int(raw[6])<<8 | int(raw[7])
		raw = raw[8:]
		if size > len(raw) {
			size = len(raw)
		}
		out.Write(raw[:size])
		raw = raw[size:]
	}
	return out.String()
}

// exec runs a command in a running container and returns its output; a
// non-zero exit is an error carrying that output.
func (d *Docker) exec(ctx context.Context, name, user string, cmd ...string) (string, error) {
	return d.execEnv(ctx, name, user, nil, cmd...)
}

// execEnv is exec with extra environment variables, which keep secrets out
// of the command line.
func (d *Docker) execEnv(ctx context.Context, name, user string, env []string, cmd ...string) (string, error) {
	var created struct{ Id string }
	if err := d.call(ctx, "POST", "/containers/"+name+"/exec", map[string]any{
		"Cmd": cmd, "User": user, "Env": env, "AttachStdout": true, "AttachStderr": true,
	}, &created); err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]any{"Detach": false, "Tty": false})
	req, err := http.NewRequestWithContext(ctx, "POST", "http://docker/exec/"+created.Id+"/start", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	out := demux(raw)
	var info struct{ ExitCode int }
	if err := d.call(ctx, "GET", "/exec/"+created.Id+"/json", nil, &info); err != nil {
		return out, err
	}
	if info.ExitCode != 0 {
		return out, fmt.Errorf("%s exited with %d: %s", cmd[0], info.ExitCode, strings.TrimSpace(out))
	}
	return out, nil
}

// Ping checks the Docker daemon is reachable.
func (d *Docker) Ping(ctx context.Context) error { return d.call(ctx, "GET", "/_ping", nil, nil) }

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return "docker: " + e.message }

func isNotFound(err error) bool {
	var e *apiError
	return errors.As(err, &e) && e.status == http.StatusNotFound
}

func (d *Docker) call(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var msg struct{ Message string }
		raw, _ := io.ReadAll(resp.Body)
		if json.Unmarshal(raw, &msg) != nil || msg.Message == "" {
			msg.Message = strings.TrimSpace(string(raw))
		}
		return &apiError{status: resp.StatusCode, message: msg.Message}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
