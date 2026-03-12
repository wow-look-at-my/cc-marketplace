---
name: html-demo
description: Create an HTML demo page to showcase or prototype something. Use when the user asks to build a demo, prototype, or interactive HTML page.
argument-hint: <thing to demo>
allowed-tools: Read, Write, Edit, Glob, Grep, Bash, Agent
---

Create an interactive HTML demo page for: **$ARGUMENTS**

## File Organization Rules

You MUST keep code separated into distinct files. This is non-negotiable.

### HTML
- Create a single `index.html` file (or a small number of HTML files if the demo truly has multiple pages)
- HTML files should contain structure only — no inline `<style>` blocks, no inline `style=""` attributes, no `<script>` blocks with logic
- Link CSS via `<link rel="stylesheet">` tags
- Link JS via `<script src="...">` tags (use `defer` or place at end of `<body>`)

### CSS
- All styles go in `.css` files, never inline
- Start with a `styles.css` or `main.css` for base/global styles
- If styles grow beyond roughly 200 lines, split into logical files (e.g., `layout.css`, `components.css`, `animations.css`, `theme.css`)
- This isn't a hard limit — use judgment, but lean toward splitting early rather than having one massive file

### JavaScript
- All logic goes in `.js` files, never in HTML
- Split JS into multiple files by responsibility — this is expected and encouraged:
  - `main.js` or `app.js` — initialization and orchestration
  - Separate files for distinct features (e.g., `chart.js`, `controls.js`, `data.js`, `animations.js`, `utils.js`)
- Keep each JS file focused on one concern
- Use simple script tags (no bundler needed) — files can reference globals or use a simple namespace pattern

### Shaders (GLSL)
- **Shaders MUST NOT go in `.js` files.** This is an absolute rule — no exceptions.
- All shader code (vertex shaders, fragment shaders, compute shaders) goes in dedicated `.glsl` files (e.g., `vertex.glsl`, `fragment.glsl`)
- Load shader files at runtime using `fetch()` — for example: `const shaderSource = await fetch('shaders/fragment.glsl').then(r => r.text());`
- Never embed shader source code as JavaScript string literals, template literals, or string constants
- Never put shaders in `<script type="x-shader/...">` tags in HTML either
- Organize shaders in a `shaders/` directory

### Directory Structure

Put all demo files in a dedicated directory. Example layout:

```
demo-name/
├── index.html
├── css/
│   ├── main.css
│   ├── layout.css
│   └── components.css
├── js/
│   ├── app.js
│   ├── controls.js
│   └── utils.js
├── shaders/         # if using WebGL/WebGPU — .glsl files loaded via fetch()
│   ├── vertex.glsl
│   └── fragment.glsl
└── assets/          # only if needed (images, data files, etc.)
```

## Server Warning

**DO NOT start a web server.** Do not run `python -m http.server`, `npx serve`, `live-server`, or any other server command. The user already has a server running to serve these files. Just create the files and tell the user where they are.

## Quality Guidelines

- Make it visually polished — use modern CSS, smooth transitions, good color choices
- Make it interactive and engaging — the point is to demo something, so it should feel alive
- Use semantic HTML elements
- Ensure it works in modern browsers without a build step
- No CDN links unless the user specifically asks for a library — prefer vanilla HTML/CSS/JS
- If the demo genuinely benefits from a library (e.g., Three.js for 3D, Chart.js for charts), ask before adding it or note that the user should grab it

## Output

After creating all files, provide:
1. A brief summary of what was built
2. The file tree showing what was created
3. How to view it (just open index.html / navigate to the directory — remember, server is already running)
