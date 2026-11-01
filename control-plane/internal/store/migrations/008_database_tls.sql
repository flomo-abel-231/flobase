-- Database connections are encrypted with TLS from 0.9.0. tls records
-- whether a project's database and pooler serve it yet, so connection
-- strings ask for TLS only where it works. MySQL makes its own certificate
-- on first start and always has.
alter table projects add column if not exists tls boolean not null default false;
update projects set tls = true where engine = 'mysql' and not tls;
