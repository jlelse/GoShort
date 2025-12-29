# GoShort

GoShort is a small, minimal URL shortener written in Go. It uses an embedded SQLite database and aims to be easy to run and operate.

---

## Quick links

- **Configuration:** See `example-config.yaml`
- **Docker image:** `ghcr.io/jlelse/goshort:latest`

---

## Running (Docker - preferred) ✅

The easiest and recommended way to run GoShort is via Docker. The official image is published to GitHub Packages.

Run the published image:

```bash
docker run -d \
  --name goshort \
  -p 8080:8080 \
  -v "$(pwd)/config:/app/config" \
  -v "$(pwd)/data:/app/data" \
  ghcr.io/jlelse/goshort:latest
```

If you prefer to build locally and run that image:

```bash
docker build -t goshort .
docker run -d \
  --name goshort \
  -p 8080:8080 \
  -v "$(pwd)/config:/app/config" \
  -v "$(pwd)/data:/app/data" \
  goshort
```

The service listens on port `8080` inside the container. Mount a `config` directory (containing `config.yaml` or `config.json`/`config.toml`) and a `data` directory for the SQLite database.

---

## Building from source (when preferred) 🔧

Building from source is useful if you want to make changes, test locally, or don't want to use Docker.

Install Go (>= 1.25) then:

```bash
# build
go build -o goshort

# run tests
go test ./...

# run locally (ensure a config is available in ./config or ./config.yaml)
./goshort
```

---

## Caddy reverse proxy (Docker Compose example) 🌐

Here's a minimal `docker-compose.yml` and `Caddyfile` that run GoShort behind Caddy (TLS + reverse proxy). Save the `docker-compose.yml` and `Caddyfile` next to your `config` and `data` directories.

`docker-compose.yml`:

```yaml
services:
  goshort:
    image: ghcr.io/jlelse/goshort:latest
    restart: unless-stopped
    volumes:
      - ./config:/app/config
      - ./data:/app/data
    expose:
      - "8080"
    networks:
      - web

  caddy:
    image: caddy:2
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    networks:
      - web

volumes:
  caddy_data:
  caddy_config:

networks:
  web:
```

`Caddyfile` (replace `yourdomain.example` with your domain):

```
yourdomain.example {
    reverse_proxy goshort:8080
}
```

Start the stack:

```bash
docker compose up -d
```

Caddy will handle TLS for you automatically and forward requests to the `goshort` service.

---

## Configuration

Configuration can be done with a simple `config.{json|yaml|toml}` file in the working directory or a subdirectory `config`.

Required config values:

* `password`: Password to create, update or delete short links
* `shortUrl`: The short base URL (without trailing slash!)
* `defaultUrl`: The default URL to which should be redirected when no slug is specified

Optional config values:

* `dbPath`: Relative path where the database should be saved

See the `example-config.yaml` file for an example configuration.

---

## Authentication

The preferred authentication method is Basic Authentication. If you try to create, modify or delete a short link, in the browser a popup will appear asking for username and password — enter just the password you configured. Alternatively you can append a URL query parameter `password` with your configured password.

---

## Usage

You can either create, update or delete short links using a browser by visiting the endpoints below, or make HTTP `POST` requests.

- Create a new short link: `/s`
    - `url`: URL to shorten
    - (optional) `slug`: the preferred slug
- Update a short link: `/u`
    - `slug`: slug to update
    - `new`: new long URL
- Delete a short link: `/d`
    - `slug`: slug to delete

---

## License

GoShort is licensed under the MIT license. See the `LICENSE` file for details.
