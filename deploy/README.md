# Deploy

The bot ships as a single image on GHCR. Images are built by
`.github/workflows/ci.yml` on every push to `master`, after the linter and the
integration tests pass.

Tags:

| Tag | Points at |
| --- | --- |
| `latest` | the newest `master` build |
| `sha-<full commit sha>` | one specific commit, for pinning and rollback |

## First run on the VPS

Put three files in one directory — `compose.yaml` from this folder, plus a `.env`
and a `config.toml` you create there:

```sh
mkdir -p /opt/loudbot && cd /opt/loudbot
curl -O https://raw.githubusercontent.com/Fragoler/blowrbot/master/deploy/compose.yaml
curl -o .env https://raw.githubusercontent.com/Fragoler/blowrbot/master/deploy/.env.example
curl -o config.toml https://raw.githubusercontent.com/Fragoler/blowrbot/master/config.example.toml
```

Fill in `.env` (`BOT_TOKEN`, `POSTGRES_PASSWORD`) and `config.toml` (channel id,
discussion chat id, moderators chat id, bot username). Leave
`[postgres] host = "postgres"` as is — that is the service name on the compose
network. Then:

```sh
docker compose up -d
docker compose logs -f bot
```

The bot applies its own migrations on startup, so there is no separate migration
step. The package is public, so no `docker login` is needed.

## Updating

```sh
docker compose pull && docker compose up -d
```

## Rolling back

Pin the previous commit in `.env` and bring the stack back up:

```sh
echo 'IMAGE_TAG=sha-<previous commit sha>' >> .env
docker compose up -d
```

A rollback only moves the binary. It does not undo migrations: if the newer
version added one, roll that back first with `task migrate:down` against the
same database.

## What lives where

- Secrets: `.env` only — never in `config.toml` and never in the image.
- Non-secret settings: `config.toml`, mounted read-only at `/app/config.toml`.
- Data: the `pgdata` volume. Back it up with `docker compose exec postgres
  pg_dump -U loudbot loudbot`.
- Postgres publishes no ports; it is reachable only from the compose network.
