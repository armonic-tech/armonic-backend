# Despliegue en una VPS

Todo en una sola máquina: Postgres, el backend, el bot de voz y Caddy (HTTPS).
Pensado para Vultr en São Paulo o Santiago; sirve para cualquier VPS con IPv4
pública directamente en la máquina (sin NAT 1:1).

## 1. La VPS

- Cloud Compute, Ubuntu 24.04, 1–2 vCPU y 2 GB de RAM, con tu clave SSH.
- **Firewall** (el de Vultr y `ufw`), abrir solo:

  | Puerto | Para |
  |---|---|
  | TCP 22 | SSH |
  | TCP 80 | certificado de Let's Encrypt |
  | TCP 443 | HTTPS y WebSocket |
  | UDP 50000 | toda la voz |

  El backend escucha en `:8080` en todas las interfaces: tiene que quedar
  **cerrado** hacia afuera; solo Caddy lo alcanza.

  ```bash
  ufw allow 22/tcp && ufw allow 80/tcp && ufw allow 443/tcp && ufw allow 50000/udp
  ufw enable
  ```

- **DNS**: un registro `A` del dominio del backend (y del cliente web, si lo
  usás) a la IP de la VPS. Con Cloudflare, nube **gris** (solo DNS): el proxy
  no pasa el UDP de la voz.

## 2. Docker

```bash
curl -fsSL https://get.docker.com | sh
```

## 3. El código

Los dos repos, uno al lado del otro, en las ramas que tienen el bot:

```bash
git clone -b feature/bot-accounts <repo-backend> armonic
git clone -b feature/grok-elevenlabs <repo-bot> armonic-bot
cd armonic/deploy
```

## 4. Configuración

```bash
cp .env.example .env                       # dominios y secretos
cp bot.env.example bot.env                 # claves de xAI y ElevenLabs
cp bot.config.yaml.example bot.config.yaml # voice_id (invite_url después)
chmod 600 .env bot.env
```

Los secretos de `.env` se generan así. `CLAIM_PASSWORD` es la contraseña de
reclamo del paso 6: **nueva**, no la de desarrollo; anotala.

```bash
openssl rand -hex 32      # JWT_SECRET
openssl rand -hex 24      # POSTGRES_PASSWORD
openssl rand -base64 18   # CLAIM_PASSWORD
```

`.env`, `bot.env` y `bot.config.yaml` están en `.gitignore`. No hace falta
`config.json`: con `CLAIM_PASSWORD` definida, el backend no lo lee.

## 5. Levantar (sin el bot todavía)

```bash
docker compose -f docker-compose.prod.yml up -d --build db app caddy
docker compose -f docker-compose.prod.yml logs -f app caddy
```

Caddy saca el certificado en el primer pedido; si falla, casi siempre es el
DNS que todavía no propagó o el puerto 80 cerrado.

## 6. Reclamar la instancia — enseguida

Desde el cliente, agregá `https://<API_DOMAIN>` y reclamala con
`CLAIM_PASSWORD`. Hasta que la reclames, cualquiera que la conozca podría
hacerlo antes.

## 7. El bot

1. En el cliente: menú del servidor > **Crear invitación para bot**.
2. Pegá el enlace en `invite_url` de `bot.config.yaml`.
3. Levantalo:

   ```bash
   docker compose -f docker-compose.prod.yml up -d --build bot
   docker compose -f docker-compose.prod.yml logs -f bot
   ```

   Tiene que decir `bot: connected, waiting for a summon`. La primera vez crea
   la cuenta con la invitación; después entra con usuario y contraseña.

## 8. Cliente web (opcional)

Si alguien va a entrar desde el navegador: `flutter build web` en el cliente,
copiá `build/web/` a `deploy/web/`, completá `APP_DOMAIN` y
`CORS_ALLOWED_ORIGINS=https://<APP_DOMAIN>` en `.env`, descomentá el bloque
del `Caddyfile` y reiniciá `app` y `caddy`. Va en un dominio aparte del
backend porque sus rutas chocarían con las de la API.

## Operación

```bash
# logs (el del backend también queda en el volumen, /app/data/armonic.log)
docker compose -f docker-compose.prod.yml logs -f --tail 100 app bot

# actualizar
git -C ../ pull && git -C ../../armonic-bot pull
docker compose -f docker-compose.prod.yml up -d --build

# backup de la base
docker compose -f docker-compose.prod.yml exec db pg_dump -U armonic armonic | gzip > armonic-$(date +%F).sql.gz
```

Los datos viven en los volúmenes `db-data` y `uploads`; los snapshots de
Vultr los cubren.

## Antes de una demo

- Probá el Wi-Fi del lugar: si bloquea UDP, la llamada queda en "conectando
  RTC". Plan B: datos móviles.
- Hacé un turno de prueba con el bot antes de empezar: la primera respuesta de
  cada sesión es la más lenta.
- Revisá `daily_budget` en `bot.config.yaml` y el saldo en xAI y ElevenLabs.
