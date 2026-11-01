package provisioner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Network restrictions limit who can reach a project's database and pooler
// ports. They touch nothing else on the server: a small container on each
// Docker host keeps one chain of rules, FLOBASE-PROJECTS, that only matches
// the published ports of restricted projects, and drops connections to them
// from addresses not on the project's list. It reapplies the rules every
// minute and whenever the host restarts, so they survive reboots.

const (
	firewallName  = "flobase-firewall"
	firewallImage = "alpine:3.20"
)

// PortRule allows only Allow (addresses or CIDRs, IPv4 or IPv6) to connect
// to a published port. Local also lets in apps on the same host: their
// containers connect from Docker's own networks, not from the server's
// address, so listing the server's IP alone would not reach them.
type PortRule struct {
	Port  int
	Allow []string
	Local bool
}

// firewallScript keeps FLOBASE-PROJECTS in step with RULES ("port cidr,cidr
// [local];port cidr"). New IPv4 connections reach published ports through Docker's
// DOCKER-USER chain; IPv6 ones arrive through Docker's proxy, so INPUT. The
// hooks only send traffic into the chain, whose rules name project ports.
const firewallScript = `set -e
apk add --no-cache -q iptables ip6tables >/dev/null
for b in nft legacy; do
  if iptables-$b -L DOCKER-USER -n >/dev/null 2>&1; then IPT=iptables-$b; IP6T=ip6tables-$b; break; fi
done
[ -n "$IPT" ] || { echo "Docker's DOCKER-USER chain is missing" >&2; exit 1; }
apply() {
  for t in "$IPT" "$IP6T"; do $t -N FLOBASE-PROJECTS 2>/dev/null || true; $t -F FLOBASE-PROJECTS; done
  echo "$RULES" | tr ';' '\n' | while read -r port cidrs local; do
    [ -n "$port" ] || continue
    if [ "$local" = local ]; then
      # Containers on this host reach published ports from Docker's bridges.
      for i in docker0 br-+; do
        $IPT -A FLOBASE-PROJECTS -i "$i" -p tcp -m conntrack --ctorigdstport "$port" -j RETURN
      done
    fi
    for c in $(echo "$cidrs" | tr ',' ' '); do
      case "$c" in
        *:*) $IP6T -A FLOBASE-PROJECTS -s "$c" -p tcp -m conntrack --ctorigdstport "$port" -j RETURN ;;
        *) $IPT -A FLOBASE-PROJECTS -s "$c" -p tcp -m conntrack --ctorigdstport "$port" -j RETURN ;;
      esac
    done
    $IPT -A FLOBASE-PROJECTS -p tcp -m conntrack --ctorigdstport "$port" -j DROP
    $IP6T -A FLOBASE-PROJECTS -p tcp -m conntrack --ctorigdstport "$port" -j DROP
  done
  hook="-m conntrack --ctstate NEW --ctdir ORIGINAL -j FLOBASE-PROJECTS"
  $IPT -C DOCKER-USER $hook 2>/dev/null || $IPT -I DOCKER-USER $hook
  $IP6T -C INPUT $hook 2>/dev/null || $IP6T -I INPUT $hook
  if $IP6T -L DOCKER-USER -n >/dev/null 2>&1; then
    $IP6T -C DOCKER-USER $hook 2>/dev/null || $IP6T -I DOCKER-USER $hook
  fi
}
apply
echo "network restrictions applied"
while sleep 60; do
  # Docker or a reboot can drop the hooks; put them back.
  $IPT -C DOCKER-USER -m conntrack --ctstate NEW --ctdir ORIGINAL -j FLOBASE-PROJECTS 2>/dev/null || apply
done`

// firewallRules turns rules into the script's RULES, sorted so the same
// rules always give the same text.
func firewallRules(rules []PortRule) string {
	sort.Slice(rules, func(i, j int) bool { return rules[i].Port < rules[j].Port })
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		if r.Port <= 0 {
			continue
		}
		allow := append([]string(nil), r.Allow...)
		sort.Strings(allow)
		part := strconv.Itoa(r.Port) + " " + strings.Join(allow, ",")
		if r.Local {
			part += " local"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ";")
}

// ApplyFirewall makes the host's network restrictions match rules. With no
// rules the chain is empty and nothing is restricted.
func (d *Docker) ApplyFirewall(ctx context.Context, rules []PortRule) error {
	text := firewallRules(rules)
	sum := sha256.Sum256([]byte(text + firewallScript + firewallImage))
	hash := hex.EncodeToString(sum[:8])
	var current struct {
		State  struct{ Running bool }
		Config struct{ Labels map[string]string }
	}
	err := d.call(ctx, "GET", "/containers/"+firewallName+"/json", nil, &current)
	if err == nil && current.State.Running && current.Config.Labels["dev.flobase.firewall"] == hash {
		return nil
	}
	// A host that never had restrictions gets no firewall container.
	if text == "" && isNotFound(err) {
		return nil
	}
	if err == nil {
		if err := d.call(ctx, "DELETE", "/containers/"+firewallName+"?force=true", nil, nil); err != nil && !isNotFound(err) {
			return fmt.Errorf("replace firewall: %w", err)
		}
	} else if !isNotFound(err) {
		return err
	}
	if err := d.pull(ctx, firewallImage); err != nil {
		return fmt.Errorf("pull %s: %w", firewallImage, err)
	}
	// An empty RULES leaves the chain empty: nothing is restricted. The
	// container keeps running so the hooks stay in place.
	config := map[string]any{
		"Image": firewallImage, "Entrypoint": []string{"sh", "-c", firewallScript},
		"Env":    []string{"RULES=" + text},
		"Labels": map[string]string{"dev.flobase.firewall": hash},
		"HostConfig": map[string]any{
			"NetworkMode":   "host",
			"CapAdd":        []string{"NET_ADMIN", "NET_RAW"},
			"RestartPolicy": map[string]any{"Name": "unless-stopped"},
			"Memory":        int64(64) * 1024 * 1024,
		},
	}
	if err := d.call(ctx, "POST", "/containers/create?name="+firewallName, config, nil); err != nil {
		return fmt.Errorf("create firewall: %w", err)
	}
	if err := d.call(ctx, "POST", "/containers/"+firewallName+"/start", nil, nil); err != nil {
		return fmt.Errorf("start firewall: %w", err)
	}
	// Wait until the rules are in, so callers can report failure.
	for i := 0; i < 60; i++ {
		out, _ := d.containerLogs(ctx, firewallName, 20, false)
		if strings.Contains(out, "network restrictions applied") {
			return nil
		}
		info, err := d.inspect(ctx, firewallName)
		if err == nil && (info.State.Status == "exited" || info.State.Status == "restarting") {
			return fmt.Errorf("the firewall could not apply the rules: %s", strings.TrimSpace(out))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("the firewall did not report within a minute")
}
