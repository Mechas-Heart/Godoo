# godoo

Odoo development environments on **Nix**, from one small Go binary.

```
godoo init shop --version 18.0
cd shop
godoo up
#   Odoo     http://localhost:8069
#   Mailpit  http://localhost:8025
#   Postgres 127.0.0.1:5433 (user odoo)
```

That one `godoo up` builds everything the project needs, then sets up and starts
its services: **PostgreSQL, Odoo, nginx and (locally) mailpit**.

- **Isolated per project.** Each project has its own Odoo version, database
  cluster, config, logs and ports. Run as many projects side by side as you
  like, for example Odoo 15 next to Odoo 18.
- **Nothing installed on your system.** Every program comes from Nix. There are
  no containers, no daemons and no system packages.
- **No virtualenv, no pip.** Python and every package Odoo imports come from
  Nix as one read-only environment in `/nix/store`. Projects on the same Odoo
  version share it.
- **Plain processes.** The services are normal processes you can see with `ps`,
  with one log file each. There is no supervisor to understand.
- **Local and prod from one project.** Locally you get mailpit, code reload
  and a debugging/linting toolset. In prod none of that is even installed.
  `GODOO_ENV=prod` switches a checkout to prod without editing `godoo.yaml`.

## Contents

- [Requirements and install](#requirements-and-install)
- [Quick start](#quick-start)
- [The services](#the-services)
- [Running as a service (systemd)](#running-as-a-service-systemd)
- [Deploying to a server](#deploying-to-a-server)
- [Local vs prod](#local-vs-prod)
- [Selecting modules: addons.yaml](#selecting-modules-addonsyaml)
- [Community or Enterprise](#community-or-enterprise)
- [Python packages and addon repos](#python-packages-and-addon-repos)
- [Several Odoo versions side by side](#several-odoo-versions-side-by-side)
- [Supported Odoo versions](#supported-odoo-versions)
- [The godoo command](#the-godoo-command)
- [Tools inside the environment](#tools-inside-the-environment)
- [Configuration: godoo.yaml](#configuration-godooyaml)
- [Project layout](#project-layout)
- [How it works](#how-it-works)
- [Debugging](#debugging)
- [Code map](#code-map)
- [Building and releasing](#building-and-releasing)
- [License](#license)

## Requirements and install

- Linux or macOS. WSL2 works.
- [Nix](https://nixos.org/download). Flakes do not need to be enabled, because
  godoo passes `--extra-experimental-features "nix-command flakes"` itself.

Install it in one of these ways:

```
# 1. with Nix (you need it anyway): no Go, no download
nix profile install github:Mechas-Heart/Godoo
nix run github:Mechas-Heart/Godoo -- init shop --version 18.0   # or just try it

# 2. the install script: the latest prebuilt binary, checksum verified,
#    into ~/.local/bin (no sudo)
curl -fsSL https://raw.githubusercontent.com/Mechas-Heart/Godoo/main/install.sh | sh

# 3. with Go (1.22+)
go install github.com/Mechas-Heart/Godoo/cmd/godoo@latest

# 4. from a clone
git clone https://github.com/Mechas-Heart/Godoo.git
cd Godoo && make install    # -> $GOPATH/bin/godoo
```

The install script takes two options, as environment variables:
`GODOO_VERSION=v0.3.0` for a specific release, and
`GODOO_INSTALL_DIR=/usr/local/bin` for another folder. You can also download
the archives yourself from the
[releases](https://github.com/Mechas-Heart/Godoo/releases) (Linux/macOS,
amd64/arm64, with `checksums.txt`).

To update, run the same command again. The install script replaces the godoo
already in the install folder and says which version it replaced. It also
warns when another godoo earlier on your PATH (for example from `go install`)
would still run instead. With Nix, run `nix profile upgrade Godoo` (the name
`nix profile list` shows).

### Uninstall

```
# installed with the script (or by copying the binary)
curl -fsSL https://raw.githubusercontent.com/Mechas-Heart/Godoo/main/install.sh | sh -s -- --uninstall

# installed with Nix
godoo down --all && nix profile remove Godoo
```

The script stops the services of every project first (`godoo down --all`),
then removes the binary. It also removes godoo's project list
(`~/.local/state/godoo`), unless some projects still exist: then the list is
kept so a reinstall still knows them.

**Your projects are never touched.** Each one keeps its code, databases and
Nix environment in its own folder. To delete a project's data, run
`godoo destroy` in it before uninstalling (or `rm -rf <project>/.godoo`
after). Then `nix store gc` frees the Nix store space godoo used.

## Quick start

```
godoo init shop --version 18.0     # creates ./shop with godoo.yaml
cd shop
godoo up                           # first run: ~5-10 min (downloads, odoo clone)
godoo db create --demo             # database "shop" with demo data
# open http://localhost:8069 and log in with admin / admin

godoo scaffold my_module           # new module in ./addons
godoo install my_module
godoo logs -f                      # follow the odoo log

godoo down                         # stop everything; data is kept
```

The first `godoo up` downloads the Nix packages (all pre-built, from
cache.nixos.org) and clones Odoo. Later runs start in a few seconds.

## The services

`godoo up` sets up and starts these four services, in this order:

| Service    | Listens on (defaults)              | What it is for |
|------------|------------------------------------|----------------|
| postgres   | `127.0.0.1:5433`                   | the project's own database cluster |
| mailpit    | SMTP `127.0.0.1:1025`, web UI `:8025` | catches every email Odoo sends (local only) |
| odoo       | `127.0.0.1:20000` (+ `20002` with workers) | `odoo-bin` from your checkout |
| nginx      | `127.0.0.1:8069`                   | the address you open in the browser, proxies to Odoo |

```
browser ──> nginx :8069 ──> odoo :20000 ──> postgres :5433
                               └── smtp ──> mailpit :1025   (web UI :8025)
```

**What "set up" means.** Before each service starts, godoo prepares it:

- **postgres:** on the first start, `initdb` creates a cluster in
  `.godoo/data/postgres`. It uses superuser `odoo`, password auth
  (scram-sha-256), UTF8 and the C locale. It listens on TCP only, with no unix
  socket. If the cluster was created by a different PostgreSQL major version,
  godoo refuses to start rather than damage it.
- **odoo:** `.godoo/conf/odoo.conf` is generated from `godoo.yaml` on every
  start. That covers database settings, `addons_path`, ports, mail and
  workers. Addon directories without any modules are left out, because Odoo
  refuses to start with them.
- **nginx:** `.godoo/conf/nginx.conf` is generated on every start. It runs as
  your user, and every file it needs (pid, temp dirs, logs) stays inside the
  project. It routes `/websocket` (or `/longpolling` before 16.0) to Odoo's
  gevent port when workers are enabled.
- **mailpit:** stores mail in `.godoo/data/mailpit.db` and accepts any SMTP
  login, so Odoo's mail server settings don't matter.

**How they run.** godoo starts each service as a normal background process,
waits until its port answers, and moves on. If a service fails to start, godoo
shows the last lines of its log. Each service has:

- a pid file: `.godoo/run/<service>.pid`
- a log file: `.godoo/logs/<service>.log`

`godoo up` is safe to run again: services already running are left alone.
`godoo down` stops them in reverse order (nginx, odoo, mailpit, postgres)
with each one's graceful-shutdown signal. Your data is kept.

**No systemd by default.** `godoo up` runs the services as plain background
processes. Nothing starts at boot and nothing restarts after a crash, so run
`godoo up` again. This works without root on WSL, macOS and any Linux. For a
server, or anything that should start at boot, see
[Running as a service (systemd)](#running-as-a-service-systemd).

**Day to day:**

```
godoo status             # state, pid, port and log file of each service
godoo logs [service] -f  # default: odoo; "all" for every log
godoo restart [service]  # default: odoo
godoo run                # odoo in the foreground, for breakpoint()/pdb
```

`godoo run` starts the other services, stops the background Odoo and replaces
itself with `odoo-bin`, so Ctrl-C and debuggers behave as usual.
`godoo run --debugpy` does the same under debugpy; see
[Local vs prod](#local-vs-prod).

## Running as a service (systemd)

```
sudo "$(command -v godoo)" service install   # as root: system units, installed and started
godoo service install --system               # as you: prints the sudo commands instead
godoo service install --user                 # user units in your account, no root
godoo service status
godoo service remove
```

`godoo service install` writes plain systemd unit files for the project:

```
godoo-shop.target              starts everything, enabled at boot
godoo-shop-postgres.service
godoo-shop-mailpit.service     (when mailpit is enabled; off in prod)
godoo-shop-odoo.service
godoo-shop-nginx.service
```

**The project then runs without godoo.** Each unit runs the same command line
as `godoo up`, with the same Nix environment, config files and log files in
`.godoo/`. No unit calls godoo, and the Nix environment is a garbage-collector
root, so `nix-collect-garbage` never removes it. systemd starts the services
at boot, in order (postgres before odoo, odoo before nginx), and restarts any
that crash. `systemctl` alone controls them:

```
sudo systemctl restart godoo-shop-odoo.service   # (systemctl --user ... for user units)
sudo systemctl stop godoo-shop.target            # stops all of them
```

godoo is only needed again to change something: new modules or packages,
another Odoo setting, other ports.

### Production runs only under systemd

In `env: prod`, godoo never runs a service as a plain background process.
`godoo up` (and every command that would start postgres, such as `godoo db
list`) stops with the three ways to install the units. A prod server starts
at boot and restarts after a crash, or it doesn't run. `godoo service install`
defaults to system units in prod and to user units in local.

### Three ways to install

| How | Units | Runs as | Who installs |
|---|---|---|---|
| `sudo godoo service install` | `/etc/systemd/system` | the project's owner, or `--run-as NAME` | godoo, as root |
| `godoo service install --system` | `/etc/systemd/system` | you | you, with the 3 `sudo` commands godoo prints |
| `godoo service install --user` | `~/.config/systemd/user` | you | godoo, no root |

**As root.** PostgreSQL refuses to run as root, so the services always run as
an account:

- The project's owner by default. For a project in your home directory, that
  is you.
- If the project belongs to root (for example `/opt/shop` set up in a root
  shell), an account called `odoo`, or the one you name with `--run-as NAME`.
  godoo creates it if it doesn't exist (a system account without login, with
  the project as its home) and asks before giving it the project
  (`chown -R`; `--yes` skips the question).

Root only installs and starts the units. Everything that writes project
files (the Nix environment, `odoo.conf`, `nginx.conf`, the PostgreSQL
cluster) runs as that account, so the services can always write their own
files. Afterwards:

```
sudo godoo up | down | restart | status        # through systemctl
sudo -u odoo godoo install sale ...            # everything else, as the account
```

godoo refuses other commands as root (they would write files as root), and
tells you the `sudo -u` form. Read-only commands (`status`, `logs`, `config`,
`doctor`, ...) always work. `sudo` often has its own `PATH`, hence
`sudo "$(command -v godoo)"`. Installing godoo to `/usr/local/bin`
(`GODOO_INSTALL_DIR=/usr/local/bin` with the install script) avoids that, and
lets other accounts such as `odoo` run it.

**System units without root access for godoo** (`--system`): godoo writes
the units into `.godoo/systemd/` and prints the `sudo cp`, `daemon-reload`
and `enable --now` commands. They run as you.

**User units** (`--user`) go into `~/.config/systemd/user`. They start when
you log in; to start them at boot without logging in, run
`loginctl enable-linger $USER` once (godoo reminds you).

### Day to day

- **godoo's own commands keep working.** Once the units are installed,
  `godoo up`, `down`, `restart` and `status` go through `systemctl`, so godoo
  and systemd never start the same service twice. Commands that change the
  config (`pkg add`, `addons add`, ...) restart odoo through systemd too.
- **Changes:** `odoo.conf` and `nginx.conf` are rewritten by `godoo up` /
  `restart`. After changing ports or turning mailpit on or off in
  `godoo.yaml`, run `godoo service install` again to rewrite the units.
- `godoo destroy` refuses while the units are installed. Run
  `godoo service remove` first (as root for system units, or it prints the
  `sudo` commands).

**Tested** on WSL2 (systemd 252) with user units, in local and prod: install
over a running `godoo up`, plain `systemctl` stop/start, recovery after
`kill -9` of odoo, `godoo up/down/restart/status` and `godoo db list` through
systemd, prod refusing plain processes, reinstall and remove. The root
guard (which commands root may run) was tested in a user namespace.
**Not tested yet:** installing system units (as root or with the printed
sudo commands), creating the `odoo` account and the `chown`. Test those on a
throwaway machine first.

## Deploying to a server

The server runs the project from git with the **same godoo version** you
used, under systemd:

```
# 1. godoo itself: the version the project is pinned to (godoo_version in godoo.yaml)
curl -fsSL https://raw.githubusercontent.com/Mechas-Heart/Godoo/main/install.sh \
  | sudo GODOO_VERSION=v0.7.1 GODOO_INSTALL_DIR=/usr/local/bin sh

# 2. the project
sudo git clone https://github.com/you/shop.git /opt/shop && cd /opt/shop

# 3. build, configure and start it under systemd (env: prod in godoo.yaml)
sudo godoo service install        # /opt/shop belongs to root, so the services run as a new `odoo` account
```

Step 3 builds the Nix environment, clones Odoo and the addon repos, writes
the configs, creates the database cluster and installs the systemd units
(see [Running as a service](#running-as-a-service-systemd)). From then on the
services run without godoo, through reboots and crashes.

**Updates** are a pull and a restart:

```
cd /opt/shop && sudo -u odoo git pull   # as the account that owns the project now
sudo -u odoo godoo setup          # new packages or addon repos, if godoo.yaml changed
sudo godoo restart                # odoo picks up the new code and config
sudo -u odoo godoo update sale    # modules whose data or views changed
```

**Why the version is pinned.** godoo writes the files the server runs
(`odoo.conf`, `nginx.conf`, `flake.nix`, the units). Another godoo version
could write them slightly differently than what you tested. So
`godoo init` records its version in `godoo.yaml`:

```yaml
godoo_version: v0.7.1
```

- A newer **bug-fix** release of the same `v0.X` (v0.8.0 -> v0.8.3) is
  accepted silently: bug-fix releases never change generated files.
- Another `v0.X` (v0.9.0 on a v0.8.x project), or an older release, warns when
  it runs the project and says how to install the pinned one.
- In `env: prod`, `godoo service install` refuses to run with such a version.
- To move to a new godoo: install it on your machine, check the project,
  run `godoo pin` (records the version you run), commit `godoo.yaml`, then
  install that version on the server.
- `godoo pin v0.7.1` sets a version explicitly. Projects made before
  `godoo_version` existed have no pin and no check until you run `godoo pin`.
- Development builds (from a clone, not a release) skip the check.

## Local vs prod

`env:` in `godoo.yaml` decides what a project gets. Everything local-only is
left out of the Nix environment in prod: it is not installed, rather than
installed and switched off.

| | `env: local` (default) | `env: prod` |
|---|---|---|
| Python + Odoo's packages, your `python_packages` | ✓ | ✓ |
| PostgreSQL, nginx, wkhtmltopdf, rtlcss, git | ✓ | ✓ |
| mailpit (catches all mail) | ✓ | – real `smtp:` instead |
| `--dev=reload,qweb,xml` + watchdog (reload on save) | ✓ | – |
| dev tools (`nix.dev_tools`) | ✓ | – |
| `nix.local_python_packages`, `nix.local_packages`, wheels with `local: true` | ✓ | – |
| workers | 0 (one process, debugger-friendly) | 2 |
| database manager (`list_db`) | on | off |
| `admin_passwd` | `admin` is fine | must be set; `admin` is refused |

**The dev tools** (`nix.dev_tools`, on by default for local) are debugpy,
ipython, ipdb, coverage, ruff, pre-commit and pgcli. What each is for and how
to run it is in [Tools inside the environment](#tools-inside-the-environment).

**Debugging in an IDE.** `godoo run --debugpy` starts Odoo in the foreground
under debugpy on `127.0.0.1:5678`. Add `--wait` to hold Odoo until the
debugger attaches, and `--debugpy-port N` when several projects run at once.
A VS Code `launch.json` entry to attach:

```json
{ "name": "godoo", "type": "debugpy", "request": "attach",
  "connect": { "host": "127.0.0.1", "port": 5678 } }
```

**Your own local-only packages:** `godoo pkg add --local pytest-odoo` (Python)
or `nix.local_packages: [httpie]` (tools). They are never part of prod.

**One project, both envs.** Commit `godoo.yaml` with `env: local` and run the
same checkout as prod on a server:

```
export GODOO_ENV=prod
export GODOO_ADMIN_PASSWD='a-long-random-secret'   # kept out of git
godoo service install          # prod runs only under systemd
```

`GODOO_ENV` and `GODOO_ADMIN_PASSWD` must be set every time godoo writes
`odoo.conf` (`service install`, `up`, `restart`), or the server gets a local
config. `sudo` drops them, so pass them along:
`sudo GODOO_ENV=prod GODOO_ADMIN_PASSWD=... "$(command -v godoo)" service install`.
On a server, `env: prod` in `godoo.yaml` is simpler and cannot be forgotten.

`godoo status` and `godoo config` show where `env` came from. Prod refuses
to start while the master password is still `admin`. Alternatively, a prod
project created with `godoo init --env prod` gets random passwords written into
its own `godoo.yaml`.

Prod gaps still to fill: TLS/HTTPS and backups. To start at boot and restart
after a crash, see [Running as a service](#running-as-a-service-systemd).

## Python packages and addon repos

Python packages and addon repos are per project and recorded in
`godoo.yaml`. A teammate who clones the project gets the same setup from
`godoo up`. These commands edit `godoo.yaml` for you, keeping your comments.
They then rebuild what changed and restart Odoo if it is running.

```
godoo pkg add pandas PyYAML          # pip-style names are fine
godoo pkg add PyPDF2==2.12.1         # an exact version (replaces the one from nixpkgs)
godoo pkg add --local pytest-odoo    # local only, never in prod
godoo pkg remove pandas
godoo pkg list                       # what you have, and what is local-only

godoo addons add web server-tools    # register OCA/web, OCA/server-tools: nothing downloaded yet
godoo addons add web --modules web_responsive   # download and use only this module
godoo addons add server-tools --all  # or all of a repo
godoo addons add acme/addons --branch main --all
godoo addons add https://gitlab.com/acme/tools.git --modules tool_a,tool_b
godoo addons deps --add              # add the python deps that addon manifests declare
godoo addons update                  # git pull --ff-only every repo
godoo addons remove web --delete
godoo addons list
```

**Where a Python package comes from**, checked in this order:

1. **Odoo's own package set.** If the package is already there, nothing is
   added.
2. **nixpkgs**, at the project's pinned version. godoo tries the usual
   spellings (`PyYAML` → `pyyaml`, `sentry_sdk` → `sentry-sdk`). The package
   goes into `nix.python_packages`.
3. **A pure-Python wheel from PyPI**, for packages nixpkgs lacks, such as
   `openupgradelib` or `odoorpc`. The wheel's URL and sha256 are pinned in
   `nix.pypi_packages` and Nix installs it, so pip is never involved. The
   wheel's own dependencies go through the same three steps.
4. Anything else is reported instead of added. A compiled package without a
   pure wheel needs a small Nix derivation.

**Package versions.** Without a version you get the one in the project's
pinned nixpkgs (`nix.python_nixpkgs`, e.g. `nixos-25.05`) or, from PyPI, the
latest release with a pure-Python wheel. To get an exact version:

```
godoo pkg add PyPDF2==2.12.1
```

- If Odoo's set or nixpkgs already has that version, it is used as usual.
- Otherwise godoo pins that version's pure-Python wheel from PyPI in
  `nix.pypi_packages`. The pinned wheel **replaces** the package of the same
  name from Odoo's set or nixpkgs, because one Python environment cannot hold
  two versions of a package. godoo warns when it replaces a package from
  Odoo's set: Odoo was tested with the other version, unless (as here) Odoo's
  `requirements.txt` asks for the pinned one.
- `godoo pkg remove PyPDF2` goes back to the version from nixpkgs.
- Only `==` works (no `>=`, `<`, `~=`): the version must be exact to be
  pinned.
- A version without a pure-Python wheel (a compiled package such as `lxml`)
  is refused. It needs a hand-written Nix derivation.

Example: nixpkgs 25.05 ships PyPDF2 3.0.1, but Odoo 19 on Python 3.12 asks for
2.12.1 and breaks on 3.x (sale order PDFs, merged PDFs).
`godoo pkg add PyPDF2==2.12.1` fixes that.

Pinned versions never change by themselves. A nixpkgs package changes only
with `godoo nix-update`, and a pinned wheel only with another `godoo pkg add
NAME==VERSION`.

**Addon repos** are registered first and downloaded only for the modules
you pick (see [`godoo addons` or `addons.yaml`?](#godoo-addons-or-addonsyaml)).
They go into `./repos/<owner>/<name>`.

- **The branch is detected.** Repositories name their branches differently,
  so without `--branch` godoo asks the remote which branches exist and picks,
  in this order:
  1. the version, e.g. `18.0` (OCA, odoo/enterprise)
  2. the major version, e.g. `18`
  3. the repository's default branch, e.g. `main`, with a warning, because it
     may not support this Odoo version

  The chosen branch is written to `godoo.yaml`. Detecting or checking a
  branch only asks the remote (`git ls-remote`), so a wrong URL or branch is
  refused before anything is written, and nothing is downloaded.

- When modules are downloaded, godoo reads each one's `external_dependencies`
  and lists the Python packages that are missing. Pass `--deps` to add them
  straight away.
- `addons remove` refuses while modules from that repo are installed in the
  default database, and `--delete` refuses when the clone has local changes,
  unpushed commits or stashes. Pass `--force` to override.
- Repos are cloned with `--filter=blob:none`. That keeps the full history, so
  `git log` and `git pull` work, but only downloads file contents for the
  checked-out branch. With `addons.yaml`, only the selected modules are
  downloaded (see [Selecting modules](#selecting-modules-addonsyaml)).

The resulting `godoo.yaml` looks like this:

```yaml
nix:
  python_packages:
    - pandas
  pypi_packages:
    - name: openupgradelib
      version: "3.13.7"
      url: https://files.pythonhosted.org/.../openupgradelib-3.13.7-py2.py3-none-any.whl
      hash: sha256-hpEPVRz5SfBihj6x3jvxWCr3fNKyvMMDHh7lI8RC41w=
repos:
  - name: web
    url: https://github.com/OCA/web.git
    branch: "18.0"
    path: ./repos/oca/web
```

## Selecting modules: addons.yaml

### `godoo addons` or `addons.yaml`?

They work together. `godoo.yaml` says **which repos** (URL, branch, folder).
`addons.yaml` says **which modules** of them are used, and only those are
downloaded. `godoo addons add` writes both, so you don't have to edit
`addons.yaml` by hand:

```
godoo addons add web server-tools                    # register: nothing downloaded
godoo addons add web --modules web_responsive        # download and use only this module
godoo addons add web --modules "web_refresh*"        # more later (wildcards work)
godoo addons add server-tools --all                  # all of a repo
godoo addons add git@github.com:acme/my-addons.git --branch main --modules my_module
godoo addons remove web                              # from godoo.yaml and addons.yaml
```

- **Without `--modules` or `--all`, nothing is downloaded.** The repo is only
  registered: it goes into `godoo.yaml`, and into `addons.yaml` as `web: []`.
  godoo checks the URL and branch with `git ls-remote`, which downloads
  nothing.
- **`--modules a,b`** downloads and uses only those modules. Run it again to
  add more. `--modules` takes one repo at a time.
- **`--all`** downloads and uses the whole repo (`web: ["*"]`). It works with
  several repos at once.
- **The first `godoo addons add` creates `addons.yaml`.** Repos you already
  had are listed there with `"*"`, so nothing that worked before turns off.
- **Downloading happens in `godoo up`**, which `godoo addons add` runs for
  you. A repo with no modules selected is never downloaded. `godoo addons
  list` shows it as "not downloaded".
- **Doodba style:** you can also name OCA repos only in `addons.yaml`, without
  `addons add`. godoo downloads their selected modules itself. `godoo addons
  add NAME` later moves such a repo into `godoo.yaml` without downloading it
  again.

Projects without `addons.yaml` (made before this, or with repos added by
hand to `godoo.yaml`) keep using every repo whole.

### The addons.yaml format

By default, every module of every addon repo is available (on `addons_path`).
To pick only some, add an `addons.yaml` next to `godoo.yaml`. It uses the same
format as [doodba](https://github.com/Tecnativa/doodba#optodoocustomsrcaddonsyaml),
so an existing doodba `addons.yaml` works as is:

```yaml
web:
  - web_responsive
  - "web_refresh*"        # wildcards: *, ?, [abc]
server-ux:                # not in godoo.yaml: downloaded from OCA automatically
  - date_range
---
ONLY:                     # this document only in local projects
  GODOO_ENV: [local]
web:
  - web_environment_ribbon
---
ENV:                      # repos below come from another place / branch
  DEFAULT_REPO_PATTERN: https://github.com/acme/{}.git
  ODOO_VERSION: "18.0"
acme-addons:
  - "*"
```

How it works, as in doodba:

- **Selected modules are symlinked** into `.godoo/addons/`, and that folder goes
  on `addons_path` instead of the whole repos. The symlinks are refreshed on
  every `godoo up`, `restart` and module command.
- **Only the selected modules are downloaded.** When a repo's entry names
  modules (not `"*"`), godoo checks out only those module folders with git's
  sparse checkout, so the rest of the repo is never downloaded. For
  `web: [web_responsive]`, `repos/oca/web` holds `web_responsive` and the
  repo's top-level files, not all 40 modules. Change `addons.yaml` and the
  next `godoo up` (or `setup`) adds or removes folders to match. `"*"`, or no
  entry, checks out the whole repo again.
- **Repos without an entry, or with an empty one (`name: []`), are inactive
  and not downloaded.** `godoo up` warns about a missing entry. `name: ["*"]`
  activates all of a repo's modules.
- **Your own addons stay available.** `odoo.addons` in `godoo.yaml` (doodba's
  `private`) stays available as a whole, as do the enterprise addons, unless
  `private:` / `enterprise:` entries select from them.
- **Core Odoo is always available.** A module selected from a repo wins over a
  core module with the same name, as in doodba.
- **Repos named only in `addons.yaml` are downloaded** from
  `DEFAULT_REPO_PATTERN` (default `https://github.com/OCA/{}.git`). Their branch
  is `ODOO_VERSION` from `ENV`, or detected like for `godoo addons add`.
- **`ONLY:`** enables a document only when every listed environment variable
  has one of the listed values. `GODOO_ENV` is the project's env (`local` or
  `prod`), even when the variable is not set in your shell.
- **Several documents** (separated by `---`) are merged.

Two differences from doodba, both to make mistakes visible:
- A pattern that matches no module gives a warning (doodba fails silently).
- A module name found in two repos gives a warning and uses the first repo
  (doodba picks one at random).

**A repo that was cloned whole** (before you added `addons.yaml`, or before
its entry listed modules) is replaced on the next `godoo up` by a fresh clone
of only the selected modules, like an update: it is at the latest commit of
the same branch. It is replaced only when nothing would be lost. With local
changes, commits that are not pushed, or stashes, godoo narrows it in place
instead and tells you why. Then the files already downloaded stay in its
`.git` until it is clean. If the new clone fails (no network, no access), the
old one is put back.

`godoo addons list` shows how many modules of each repo are checked out
(`2 of 40`) and active, and
`godoo addons deps` / `godoo doctor` check only the selected modules. Delete
`addons.yaml` to go back to whole repos.

## Community or Enterprise

Projects run Odoo Community by default. Enterprise is Community plus the
**enterprise addons repository**. You need access to it through an Odoo
Enterprise subscription or partnership: `odoo/enterprise` on GitHub, or your
company's mirror of it.

```
godoo edition enterprise                                  # git@github.com:odoo/enterprise.git
godoo edition enterprise --repo git@github.com:acme/odoo-enterprise.git
godoo edition enterprise --branch 18.0                    # skip detection
godoo edition                                             # show the current edition
godoo edition community                                   # switch back
godoo init shop --version 18.0 --edition enterprise       # from the start
```

What `godoo edition enterprise` does:

1. Finds the branch, the same way as for addon repos: `18.0`, then `18`, then
   the default branch (`main`).
2. Clones it into `./enterprise`. A wrong URL or missing access fails here,
   before anything is written.
3. Writes `odoo.edition` and `odoo.enterprise` to `godoo.yaml`.
4. Puts `enterprise/` first in `addons_path` (Odoo's recommended order) and
   restarts Odoo.

```yaml
odoo:
  edition: enterprise
  enterprise:
    url: git@github.com:odoo/enterprise.git
    branch: "18.0"
    path: ./enterprise
```

- **Databases.** `godoo db create` installs `web_enterprise` in enterprise
  mode. To turn an existing Community database into an Enterprise one, run
  `godoo install web_enterprise -d DB`.
- **Switching back.** `godoo edition community` refuses while enterprise
  modules are installed in the default database, because that database would
  break without their code. `--force` overrides it. The `./enterprise`
  checkout is kept.
- **Teammates.** With `edition: enterprise` in `godoo.yaml`, `godoo up` clones
  the enterprise addons for them, using their own access.
- **Keep it private.** Odoo Enterprise is proprietary. The generated
  `.gitignore` excludes `enterprise/`. Never commit it, least of all to a
  public repository.
- **Private repositories need your credentials.** git uses your SSH key or
  credential helper. If the key has a passphrase, git asks for it in your
  terminal.

## Several Odoo versions side by side

```
godoo init legacy --version 15.0     # http 8069, postgres 5433, mailpit 8025
godoo init shop   --version 18.0     # http 8070, postgres 5434, mailpit 8026
godoo -C legacy up
godoo -C shop up
godoo list
```

`init` gives each project its own block of ports, written to `ports:` in
`godoo.yaml`. It records them in `~/.local/state/godoo/instances.json`, so a
stopped project keeps its ports. `godoo list` shows every project and whether
it is running.

To share one Odoo checkout between projects of the same version, use
`godoo init --source /path/to/odoo` or set `odoo.source`.

## Supported Odoo versions

| Odoo | Python packages from          | Python | PostgreSQL | Tested |
|------|-------------------------------|--------|------------|--------|
| 15.0 | nixos-24.11 `odoo15`          | 3.10   | 14         | end to end |
| 16.0 | nixos-25.05 `odoo16`          | 3.11   | 15         | requirements check |
| 17.0 | nixos-25.05 `odoo17`          | 3.11   | 16         | requirements check |
| 18.0 | nixos-25.05 `odoo` + 3 extras | 3.12   | 16         | end to end |
| 19.0 | nixos-25.05 `odoo` + 4 extras | 3.12   | 17         | end to end |

"End to end" means a database was created, apps were installed (sale,
website, stock and OCA modules), pages were served through nginx and email
reached mailpit. "Requirements check" means every package in that branch's
`requirements.txt` is present in the Nix Python.

nixpkgs packages Odoo itself, and godoo reuses that package's list of Python
dependencies. The list includes the version fixes nixpkgs applies, for
example docutils 0.17 for Odoo 17, so the set is known to work with that Odoo
series. Odoo itself still runs from your git checkout. PostgreSQL, nginx,
mailpit, git, wkhtmltopdf and rtlcss come from `nixos-25.05`.

Package versions come from nixpkgs, not from the exact pins in Odoo's
`requirements.txt`. `godoo doctor` checks that every requirement is present;
`--versions` lists them side by side. When a version difference breaks
something, pin Odoo's version with `godoo pkg add NAME==VERSION` (see
[Package versions](#python-packages-and-addon-repos)).

Odoo 11 to 14 are not supported. nixpkgs never packaged them, so there is no
tested package set to build on.

The table lives in `internal/versions/versions.go`. Supporting a new Odoo
release means adding one row.

## The godoo command

```
godoo [-v] [-C DIR] <command> [arguments] [flags] [-- passthrough]
```

- `-v` prints every command godoo runs (nix, git, psql, odoo-bin, ...).
- `-C DIR` works on the project in DIR instead of the current directory.
  Without it, godoo looks for `godoo.yaml` in the current directory and its
  parents, like git does.
- Flags can come before or after arguments. Everything after `--` is passed
  unchanged to the underlying program (`odoo-bin`, `psql`).
- `godoo help` lists all commands. `godoo <command> -h` shows a command's
  flags.

### Projects

| Command | What it does |
|---|---|
| `godoo init [DIR]` | create a project: `godoo.yaml`, `nix/flake.nix`, `addons/`, `.gitignore`; picks free ports |
| | `--version 18.0` (15.0–19.0) · `--env local\|prod` · `--edition community\|enterprise` · `--enterprise-repo URL` · `--name NAME` · `--source PATH` (existing odoo checkout) |
| `godoo edition [community\|enterprise]` | show or switch the edition: `--repo URL` · `--branch B` · `--path DIR` · `--force` ([details](#community-or-enterprise)) |
| `godoo list [--prune]` | every project on this machine, its Odoo version and whether it runs; `--prune` forgets deleted ones |
| `godoo config` | the fully resolved `godoo.yaml` (defaults filled in) and where `env` came from |
| `godoo doctor [--versions]` | check nix, Odoo's requirements vs the Nix Python, addon dependencies, ports |
| `godoo destroy [--yes]` | stop everything and delete `.godoo/` (**all databases and filestore**); keeps your code |
| `godoo pin [VERSION]` | record the godoo version the project is made with (default: the one running); [details](#deploying-to-a-server) |
| `godoo version` | godoo's version |

### Services

| Command | What it does |
|---|---|
| `godoo up` | build what is missing, then start postgres, mailpit, odoo, nginx |
| `godoo down [--all]` | stop all services; data is kept (`--all`: of every project on this machine) |
| `godoo status` | state, pid, port and log file of each service |
| `godoo service install [--user\|--system] [--run-as NAME] \| remove \| status` | run the project under systemd: starts at boot, restarts on failure, works without godoo; the only way to run env prod ([details](#running-as-a-service-systemd)) |
| `godoo restart [SERVICE]` | restart one service: `odoo` (default), `postgres`, `nginx`, `mailpit` |
| `godoo logs [SERVICE\|all] [-f] [-n 100]` | show a log (default `odoo`); `-f` follows it |
| `godoo run [-- ODOO_ARGS]` | Odoo in the foreground instead of the background, for `breakpoint()` and pdb |
| | `--debugpy` (attach an IDE debugger) · `--wait` (hold until it attaches) · `--debugpy-port 5678` |

### Odoo

| Command | Runs |
|---|---|
| `godoo install MODULES [-d DB]` | `odoo-bin -d DB -i MODULES --stop-after-init --no-http` |
| `godoo update MODULES [-d DB]` | `odoo-bin -d DB -u MODULES --stop-after-init --no-http` |
| `godoo test MODULES [-d DB] [--tags T]` | install/update with `--test-enable` (and `--test-tags T`) |
| `godoo shell [-d DB]` | `odoo-bin shell` (IPython locally) |
| `godoo scaffold NAME` | `odoo-bin scaffold NAME` into the first addons directory |
| `godoo odoo -- ARGS` | `odoo-bin ARGS` with the project's config, e.g. `godoo odoo -- --help` |

`MODULES` can be comma or space separated (`sale,stock` or `sale stock`).
`DB` defaults to `odoo.db` in `godoo.yaml` (the project name). These commands
start postgres (and mailpit) if needed, and work whether or not the Odoo
server is running.

### Databases

| Command | What it does |
|---|---|
| `godoo db list` | databases in the project's postgres |
| `godoo db create [NAME]` | new database with `base`, plus `web_enterprise` for Enterprise (NAME defaults to `odoo.db`) |
| | `--demo` (demo data) · `--lang fr_FR` · `-i sale,stock` (more modules) |
| `godoo db drop NAME [--yes]` | drop a database and its filestore (asks first) |
| `godoo psql [DB] [-- PSQL_ARGS]` | psql connected to the project's postgres (DB defaults to `odoo.db`) |

### Packages and addon repos

These edit `godoo.yaml`, rebuild what changed and restart Odoo if it runs. See
[Python packages and addon repos](#python-packages-and-addon-repos).

| Command | What it does |
|---|---|
| `godoo pkg add NAME[==VERSION]... [--local]` | add Python packages (pip-style names); `==VERSION`: that exact version ([details](#python-packages-and-addon-repos)); `--local`: never in prod |
| `godoo pkg remove NAME...` | remove them |
| `godoo pkg list` | the project's packages, where each comes from, which are local-only |
| `godoo addons add REPO...` | register addon repos (nothing downloaded); the branch is detected (`18.0`, `18`, then the default branch) ([details](#godoo-addons-or-addonsyaml)) |
| | `--modules a,b`: download and use only these · `--all`: the whole repo · REPO: `web` (= OCA/web) · `owner/repo` · a git URL · `--branch B` · `--path DIR` · `--deps` |
| `godoo addons update [NAME...]` | `git pull --ff-only` the repos (all by default) |
| `godoo addons remove NAME... [--delete] [--force]` | drop repos from `godoo.yaml` and `addons.yaml` (`--delete` also removes the clone, unless it has local changes, unpushed commits or stashes) |
| `godoo addons list` | repos with branch, commit, module count and how many are active ([addons.yaml](#selecting-modules-addonsyaml)) |
| `godoo addons deps [--add]` | Python packages the addons' manifests need but are missing |

### Environment

| Command | What it does |
|---|---|
| `godoo setup` | build the Nix environment, clone Odoo and the addon repos (`up` does this when needed) |
| `godoo nix-update` | move `nix/flake.lock` to the latest packages of the pinned nixpkgs, then rebuild |
| `godoo sh` | a subshell with the project's tools on `PATH` (`exit` to leave) |
| `godoo env` | the same environment as `export` lines: `eval "$(godoo env)"` |

## Tools inside the environment

Each project's tools live in its Nix environment (`.godoo/env/bin`). They are
**not on your normal `PATH`**: that is what keeps projects and Odoo versions
apart. A project on Odoo 15 has its own `python3` and `psql`, which differ
from the ones in an Odoo 18 project. There are three ways to use them:

```
godoo psql shop                 # 1. through a godoo command (when one exists)

godoo sh                        # 2. a subshell with the project's tools first on PATH
ruff check addons/
exit

eval "$(godoo env)"             # 3. load them into your current shell
```

`godoo sh` and `godoo env` also set `PGHOST`, `PGPORT`, `PGUSER` and
`PGPASSWORD` (so `psql`, `pgcli`, `pg_dump` reach the project's postgres
without flags) and `ODOO_RC` (the generated `odoo.conf`). `PGDATABASE` is
deliberately not set, so name the database: `psql shop`, `pgcli shop`.

| Tool | local | prod | Use it for | Example (inside `godoo sh`) |
|---|:-:|:-:|---|---|
| `python3` | ✓ | ✓ | Python with all of Odoo's packages | `python3 odoo/odoo-bin --version` |
| `psql`, `pg_dump`, `pg_restore`, `createdb`, `dropdb` | ✓ | ✓ | PostgreSQL client tools | `pg_dump -Fc shop > shop.dump` |
| `git` | ✓ | ✓ | used for the odoo and addon repos | |
| `wkhtmltopdf` | ✓ | ✓ | PDF reports (Odoo calls it) | |
| `rtlcss` | ✓ | ✓ | right-to-left languages (Odoo calls it) | |
| `postgres`, `nginx` | ✓ | ✓ | run by godoo as services | |
| `mailpit` | ✓ | – | run by godoo as a service | |
| `ipython` | ✓ | – | interactive Python; also used by `godoo shell` | `ipython` |
| `debugpy` | ✓ | – | IDE debugging, via `godoo run --debugpy` | |
| `ipdb` (Python module) | ✓ | – | `import ipdb; ipdb.set_trace()` in your code | |
| `coverage` | ✓ | – | test coverage | `coverage run odoo/odoo-bin -c $ODOO_RC -d shop -u my_module --test-enable --stop-after-init --no-http` |
| `ruff` | ✓ | – | lint and format Python | `ruff check addons/` · `ruff format addons/` |
| `pre-commit` | ✓ | – | OCA-style hooks in a repo that has `.pre-commit-config.yaml` | `cd repos/oca/web && pre-commit run --all-files` |
| `pgcli` | ✓ | – | psql with autocompletion | `pgcli shop` |
| your `nix.packages` | ✓ | ✓ | any other tool from nixpkgs | |
| your `nix.local_packages` | ✓ | – | any other dev tool from nixpkgs | |

Notes:
- **There is no `odoo` command.** Odoo runs from your checkout as
  `odoo/odoo-bin`. Use `godoo odoo -- ARGS`, which adds the right Python and
  `-c odoo.conf`, or call `python3 odoo/odoo-bin -c $ODOO_RC` inside
  `godoo sh`.
- The local-only tools come from `nix.dev_tools` (on by default; set it to
  `false` to drop them) and from your `local_*` lists. See
  [Local vs prod](#local-vs-prod).
- `pre-commit` downloads its hook tools itself, into `~/.cache/pre-commit`,
  the first time it runs in a repo.

## Configuration: godoo.yaml

`godoo init` writes a fully commented `godoo.yaml`. Values you leave out get a
default for your Odoo version. `godoo config` prints the resolved result. The
main sections:

| Section    | Holds |
|------------|-------|
| `odoo`     | version, edition + enterprise repo, source checkout, local addons dirs, default db, dev mode, workers, extra `odoo.conf` options |
| `ports`    | one port per service (assigned by `init`) |
| `postgres` | version, user, password |
| `nginx`    | listen address (`0.0.0.0` to reach it from your network), server name, upload size |
| `mailpit`  | on/off (defaults: on for local, off for prod) |
| `smtp`     | real outgoing mail server when mailpit is off |
| `repos`    | addon repositories (`godoo addons ...`) |
| `env`      | `local` or `prod` ([Local vs prod](#local-vs-prod)); `GODOO_ENV` overrides it |
| `nix`      | nixpkgs pins, `python_packages`, `pypi_packages`, `packages`; local-only: `dev_tools`, `local_python_packages`, `local_packages` |

After editing `godoo.yaml` by hand, run `godoo up`. It regenerates the configs
and rebuilds the Nix environment if needed. A service that is already running
keeps its old settings until you restart it with `godoo restart <service>`.

## Project layout

```
shop/
├── godoo.yaml          # your settings - the only file you normally edit
├── addons.yaml         # optional: which modules of which repos (doodba format)
├── addons/             # your modules
├── repos/oca/web/      # addon repos (godoo addons add)
├── enterprise/         # enterprise addons (edition: enterprise) - never commit
├── odoo/               # odoo source checkout (git, --depth 1)
├── nix/
│   ├── flake.nix       # generated from godoo.yaml + env (gitignored)
│   └── flake.lock      # exact nixpkgs revisions - commit this
└── .godoo/             # generated state; `godoo destroy` deletes it
    ├── env -> /nix/store/...-godoo-shop-env   python + packages, postgres, nginx, ...
    ├── addons/         # symlinks to the modules selected in addons.yaml
    ├── conf/           # odoo.conf, nginx.conf (regenerated on every start)
    ├── data/           # postgres cluster, odoo filestore, mailpit.db
    ├── logs/           # one log per service
    ├── run/            # pid files, nginx temp dirs
    └── systemd/        # unit files from `godoo service install`
```

The generated `.gitignore` excludes `.godoo/`, `odoo/`, `repos/`,
`enterprise/` and `nix/flake.nix`, because `godoo up` recreates them from
`godoo.yaml`. Enterprise in particular must stay out of git.
`flake.nix` differs between local and prod, so committing it would make a prod
checkout look modified. Commit `nix/flake.lock`: it pins the exact package
versions.

## How it works

1. **Nix environment.** `nix/flake.nix` builds a single environment and links
   it to `.godoo/env`. That link is also a GC root, so `nix-collect-garbage`
   leaves it alone. The environment holds:
   - Python with Odoo's packages
   - PostgreSQL
   - nginx
   - git
   - wkhtmltopdf
   - rtlcss
   - local only: mailpit and the dev tools

   Everything comes pre-built from cache.nixos.org.
2. **Python.** The flake uses `python.withPackages` with these packages:
   - the dependency list of nixpkgs' Odoo package
   - your `python_packages` and `pypi_packages`
   - local only: watchdog, debugpy, ipython, ipdb, coverage and your
     `local_python_packages`

   The local-only lists appear separately in the flake (`localPythonPackages`,
   `localPackages`). In prod they are empty, so you can see what was left out.

   One package is replaced where nixpkgs breaks Odoo: the commented `fixes`
   block in the flake. nixpkgs 25.05 ships passlib 1.9 (the libpass fork),
   which prints every Odoo log line twice, so godoo uses passlib 1.7.4, the
   version Odoo pins.
3. **Services.** godoo starts the services as described in
   [The services](#the-services). Its whole process supervisor is
   `internal/proc/proc.go`, about 150 lines.

**Environment variables.** godoo starts every process with the project's
environment:
- `PATH` puts the Nix environment first.
- `PGHOST`, `PGPORT`, `PGUSER` and `PGPASSWORD` point at the project's
  postgres.
- `ODOO_RC` points at the generated `odoo.conf`.

It also drops some variables from your shell:
- `PYTHONPATH`, `VIRTUAL_ENV` and `LD_LIBRARY_PATH`, which would leak packages
  from outside the environment into it.
- `PGDATABASE`, because Odoo 19 reads it as the default database.

## Debugging

- `godoo -v up` prints every `nix`, `git` and service command it runs.
- `godoo status` shows each service's log file. `godoo logs all -f` follows
  all of them.
- `godoo doctor` checks Nix, missing requirements, addon dependencies and port
  conflicts.
- `eval "$(godoo env)"` gives your own shell the project's `python3` (with all
  of Odoo's packages), plus `psql` already pointed at the right port and user,
  and `ODOO_RC`. `godoo sh` opens a subshell with the same environment.
- You can use the flake without godoo: `nix build path:./nix -o .godoo/env`,
  or just the Python with `nix build path:./nix#python`.
- Every generated file starts with a comment saying where it comes from.
- A port already in use is reported by name. Change it under `ports:` in
  `godoo.yaml`.

## Code map

```
cmd/godoo/              main()
flake.nix               godoo itself as a Nix package (nix run / nix profile install)
install.sh              the curl | sh installer for release binaries
internal/cli/           one file per command group (cmd_*.go); root.go = dispatch;
                        pkg_plan.go = where each python package comes from
internal/config/        godoo.yaml types, defaults, validation, ports, in-place edits
internal/versions/      per-Odoo-version knowledge (nixpkgs pin, packages, flags)
internal/project/       paths inside a project + the process environment
internal/nix/           flake rendering, nix build/eval, python package lookup
internal/pypi/          pure-python wheels from PyPI for what nixpkgs lacks
internal/odoo/          odoo + repo clones (sparse.go: only selected modules), addons_path, odoo.conf, odoo-bin argv, dependency checks
internal/selection/     addons.yaml (doodba format): which modules of which repos
internal/services/      postgres.go, mailpit.go, odoo.go, nginx.go + manager.go;
                        systemd.go hands start/stop to systemctl once units are installed
internal/cli/runas.go   running as root: which commands may run, preparing as the service account
internal/systemd/       unit files (units.go) and systemctl (systemctl.go)
internal/proc/          start/stop/wait for background processes
internal/registry/      the list of projects on this machine
internal/templates/     *.tmpl for flake.nix, odoo.conf, nginx.conf, godoo.yaml
```

## Building and releasing

```
make build    # bin/godoo
make test     # go test ./...
make dist     # static binaries for linux/darwin, amd64/arm64, in dist/

nix build     # ./result/bin/godoo, built by the flake
nix develop   # a shell with Go, make and gopls, if Go is not installed
```

**CI** (`.github/workflows/ci.yml`) runs on every push and pull request:
- gofmt, the license-header check, vet, the tests and a build.
- `nix build` of godoo's own flake, and shellcheck on `install.sh`.
- For every supported Odoo version, it builds the Nix Python and checks it
  against that branch's real `requirements.txt`.

**Releases** (`.github/workflows/release.yml`): pushing a tag builds the
static binaries and attaches them to a GitHub release. `install.sh` always
fetches from there.

**Version numbers.** Projects pin the godoo version they are made with (see
[Deploying to a server](#deploying-to-a-server)), so the numbers carry a
promise:

- `v0.8.0 -> v0.8.1`: **bug fixes only.** Nothing godoo generates changes
  (`flake.nix`, `odoo.conf`, `nginx.conf`, units). Pinned projects accept it
  without a warning.
- `v0.8.x -> v0.9.0`: **new features**, or anything that changes generated
  files. Pinned projects warn until someone tests them and runs `godoo pin`.

To release, first set the new version in `flake.nix` (`version = "0.8.0";`),
commit, then tag that commit:

```
git tag v0.8.0 && git push origin v0.8.0
```

The release workflow stops if the tag and `flake.nix` disagree.

**When `go.sum` changes** (a new or updated Go dependency), `nix build` fails
and prints the new hash. Put it in `vendorHash` in `flake.nix`.

**Version numbers.** `make` sets the version from the git tag. The flake uses
its `version`. `go install ...@vX.Y.Z` and a plain `go build` report the
version and commit that Go records in the binary.

## License

godoo is licensed under the [Apache License 2.0](LICENSE).
Copyright 2026 Mechas Heart. See [NOTICE](NOTICE).

Every source file starts with:

```go
// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0
```

CI fails if a new `.go` or `.py` file is missing it.

- **Files godoo generates are yours.** `godoo.yaml`, `nix/flake.nix`,
  `odoo.conf`, `nginx.conf` and everything else godoo writes into your project
  may be used, changed and shared however you like, with no license conditions
  from godoo.
- **What godoo runs keeps its own license.** godoo does not include Odoo,
  OCA modules or nixpkgs packages. It downloads and runs them, and each is
  covered by its own license: Odoo Community is LGPL-3.0, and most OCA modules
  are AGPL-3.0 or LGPL-3.0.
