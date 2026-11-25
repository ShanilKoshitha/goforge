# Quick Start

[Documentation home](index.md)

Create a GoForge app, run it against PostgreSQL, and add an issue through the
generated browser interface. Run the commands in a terminal from the directory
where you want to create the app.

## Prerequisites

- Go 1.25 or newer. Check with `go version`.
- A running Docker engine with Docker Compose. Check with `docker info` and
  `docker compose version`. The generated Compose file starts PostgreSQL and
  Mailpit for local development.
- A browser and free local ports 8080, 5432, 1025, and 8025.

## 1. Install Forge

These examples use Forge v0.23.1.

### Windows

Download the PowerShell installer and review it before running it. The script
adds Forge's binary directory to your current and user `PATH`.

```powershell
$installer = Join-Path $env:TEMP 'goforge-install.ps1'
Invoke-WebRequest 'https://raw.githubusercontent.com/ShanilKoshitha/goforge/v0.23.1/scripts/install.ps1' -OutFile $installer
Get-Content $installer
```

```powershell
& $installer -Version v0.23.1
forge version
```

If your Windows configuration blocks the persistent `PATH` update, the
installer prints the directory to add to your user `PATH` manually. You can
also install with Go directly and add its binary directory to `PATH`. Use
`go env GOBIN` to find it, or use the `bin` directory under the first entry
of `go env GOPATH` if `GOBIN` is empty. [Installer source](../scripts/install.ps1)

### macOS and Linux

```sh
go install -v github.com/ShanilKoshitha/goforge/cmd/forge@v0.23.1
gobin="$(go env GOBIN)"
if [ -z "$gobin" ]; then
  gopath="$(go env GOPATH)"
  gobin="${gopath%%:*}/bin"
fi
export PATH="$gobin:$PATH"
forge version
```

The export adds Forge to the current shell. Add the binary directory to your
shell profile to keep it available in future terminals. On macOS, `forge dev`
also uses the system `lsof` command to verify the watched server.

## 2. Create the project and start its services

The remaining commands work in PowerShell and POSIX shells.

```sh
forge new myapp --module example.com/myapp
cd myapp
docker compose up -d --wait
```

Open the generated `.env` and change these two lines for local HTTP:

```dotenv
APP_ENV=local
APP_URL=http://localhost:8080
```

Keep the generated secrets and database URL. Use these local settings only on
your development machine. They allow HTTP session cookies and the local
Mailpit configuration.

## 3. Generate and run an Issue resource

```sh
forge make:resource Issue --field title:string
forge migrate
forge dev
```

The generator writes the Issue model, PostgreSQL migration, JSON and browser
controllers, and views. `forge migrate` applies the base and Issue migrations.
`forge dev` starts the watched web server, queue worker, and scheduler.

When Forge reports that the stack is ready, open the registration page and
create a local account. Then open Issues, select **New Issue**, enter
`Fix signup copy` as the title, and select **Create Issue**. The Issue page
should show the title you entered.
[Register](http://localhost:8080/register) · [Issues](http://localhost:8080/app/issues)

If startup fails, run `forge doctor` from another terminal in the app
directory. It checks configuration, PostgreSQL and migration state, and
generated artifacts. [Diagnostics](diagnostics.md)

## 4. Make your first code change

The Issue list lives in `resources/views/pages/issues/index.forge.html`.
Change `title=.Title` to `title="Issue tracker"` on the `components/card`
line and save the file. With `forge dev` running, the browser reloads after
Forge builds and starts the updated server. The Issue list heading should now
say **Issue tracker**. Find the rest of the generated Issue code in
`internal/resources/issue`.

## 5. Test and build

Stop `forge dev` with Ctrl+C. From the app directory, run:

```sh
forge test
forge build
```

The build writes the server executable to `bin/app` on macOS and Linux or
`bin/app.exe` on Windows. Run `docker compose down` when you are finished with
the local services. Compose keeps the database volume for your next session.

## Next steps

- Add your own endpoint or customize the generated CRUD. [Building an application](building-an-app.md)
- Learn how to change the Issue templates and add components. [Forge view language](view-language.md)
- Look up generators, compatibility, and direct Go commands. [Forge CLI](cli.md)
