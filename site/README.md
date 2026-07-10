# Manual de usuario de Armonic (sitio estático)

Sitio HTML estático, sin dependencias ni build, listo para **GitHub Pages**.

```
site/
├── index.html        Portada + navegación
├── guia.html         Guía de uso (miembros + admin)
├── notas.html        Notas de liberación (changelog)
├── faq.html          Preguntas frecuentes
├── roadmap.html      Estado del proyecto y limitaciones conocidas
├── assets/
│   └── styles.css    Estilos compartidos por todas las páginas
└── README.md         Este archivo
```

## Ver localmente

Abrí cualquier `.html` en el navegador, o serví la carpeta:

```bash
cd site && python3 -m http.server 8000
# luego abrí http://localhost:8000
```

## Publicar en GitHub Pages

**Opción A — carpeta `/site` no es soportada directamente por Pages** (solo admite la raíz o `/docs`). Elegí una:

- **Rama dedicada:** publicá el contenido de `site/` en una rama `gh-pages` y en *Settings → Pages* seleccioná esa rama, carpeta `/ (root)`.
- **Carpeta `/docs`:** renombrá o copiá `site/` a `docs/` y en *Settings → Pages* elegí la rama `main`, carpeta `/docs`.
- **Workflow de Actions:** subí `site/` como artefacto con la acción oficial `actions/upload-pages-artifact` apuntando a `./site`.

El archivo `.nojekyll` ya incluido evita que GitHub Pages procese el sitio con Jekyll.

## Agregar una página nueva

1. Copiá cualquier página existente (ej. `faq.html`) a `nueva.html`.
2. Cambiá el `<title>` y el contenido dentro de `<main>`.
3. En el bloque `.nav-links` de **todas** las páginas, agregá el enlace:
   ```html
   <a href="nueva.html">Mi sección</a>
   ```
   y marcá con `class="active"` el enlace correspondiente en la página nueva.

No hace falta tocar `assets/styles.css`: ya trae utilidades para tarjetas (`.card`, `.grid`),
pasos (`.steps`), avisos (`.callout note|warn`), tablas, badges (`.tag member|admin`),
changelog (`.changelog`), acordeones de FAQ (`<details class="faq">`) y listas de estado
(`.status-list` con `.dot done|wip|planned|gap`). El tema claro/oscuro es automático.
