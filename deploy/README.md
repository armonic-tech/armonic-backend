# Despliegue en una VPS

Todo en una sola máquina: Postgres, el backend, Caddy (HTTPS) y, si se usa,
el cliente web. Pensado para Vultr en São Paulo o Santiago; sirve para
cualquier VPS con IPv4 pública directamente en la máquina (sin NAT 1:1).

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

- **DNS**: un registro `A` por dominio a la IP de la VPS: el del backend
  (`API_DOMAIN`) y, si usás el cliente web, el suyo (`APP_DOMAIN`). Son dos
  dominios distintos porque las rutas del cliente chocarían con las de la API.
  Con Cloudflare, nube **gris** (solo DNS): el proxy no pasa el UDP de la voz.

## 2. Docker

```bash
curl -fsSL https://get.docker.com | sh
```

## 3. El código

```bash
git clone -b <rama> <repo-backend> armonic
cd armonic/deploy
```

## 4. Configuración

```bash
cp .env.example .env
chmod 600 .env
```

Completá los dominios y generá los secretos. `CLAIM_PASSWORD` es la
contraseña de reclamo del paso 7: **nueva**, no la de desarrollo; anotala.

```bash
openssl rand -hex 32      # JWT_SECRET
openssl rand -hex 24      # POSTGRES_PASSWORD
openssl rand -base64 18   # CLAIM_PASSWORD
```

Con cliente web: `APP_DOMAIN=<dominio del cliente>` y
`CORS_ALLOWED_ORIGINS=https://<APP_DOMAIN>`. Sin él, dejá los dos vacíos.

`.env` está en `.gitignore`. No hace falta `config.json`: con
`CLAIM_PASSWORD` definida, el backend no lo lee.

## 5. El cliente web (opcional)

Se compila en tu máquina, no en la VPS (Flutter no entra cómodo en 2 GB):

```bash
# en el repo del cliente, en la rama a desplegar
flutter build web --release
rsync -a --delete build/web/ root@<IP>:armonic/deploy/web/
```

Después descomentá el bloque `{$APP_DOMAIN}` del `Caddyfile`. Para
actualizarlo más adelante alcanza con volver a compilar y correr el `rsync`:
Caddy sirve los archivos nuevos sin reiniciar.

## 6. Levantar

```bash
docker compose -f docker-compose.prod.yml up -d --build
docker compose -f docker-compose.prod.yml logs -f app caddy
```

Caddy saca los certificados en el primer pedido a cada dominio; si falla,
casi siempre es el DNS que todavía no propagó o el puerto 80 cerrado.

## 7. Reclamar la instancia — enseguida

Desde el cliente, agregá `https://<API_DOMAIN>` y reclamala con
`CLAIM_PASSWORD`. Hasta que la reclames, cualquiera que la conozca podría
hacerlo antes.

## Operación

```bash
# logs (el del backend también queda en el volumen, /app/data/armonic.log)
docker compose -f docker-compose.prod.yml logs -f --tail 100 app

# actualizar el backend
git -C .. pull
docker compose -f docker-compose.prod.yml up -d --build app

# backup de la base
docker compose -f docker-compose.prod.yml exec db pg_dump -U armonic armonic | gzip > armonic-$(date +%F).sql.gz
```

Los datos viven en los volúmenes `db-data` y `uploads`; los snapshots de
Vultr los cubren.

## Antes de una demo

- Probá el Wi-Fi del lugar: si bloquea UDP, la llamada queda en "conectando
  RTC". Plan B: datos móviles.
- El cifrado de un dispositivo nuevo lo habilita un miembro del grupo que
  esté conectado (`docs/ai/e2ee.md`): dejá abierta una sesión tuya en la que
  ya leas los mensajes cifrados mientras la gente se suma.
