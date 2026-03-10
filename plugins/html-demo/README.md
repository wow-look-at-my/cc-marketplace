# HTML Demo Plugin

Create organized, well-structured HTML demo pages with properly separated CSS and JS files.

## Installation

```bash
# Add the marketplace (if not already added)
claude plugin marketplace add wow-look-at-my-code/cc-marketplace#latest

# Install this plugin
claude plugin install html-demo
```

## Features

### Skills

- `html-demo` - Creates an interactive HTML demo page for any concept, with code properly organized into separate HTML, CSS, and JS files.

## Usage

```
/html-demo a particle system with gravity controls
/html-demo a kanban board with drag and drop
/html-demo a color palette generator
```

The skill will create a clean directory structure:

```
demo-name/
├── index.html
├── css/
│   ├── main.css
│   └── components.css
├── js/
│   ├── app.js
│   └── controls.js
└── assets/
```

## Notes

- Assumes you already have a local server running to serve files
- Prefers vanilla HTML/CSS/JS — no build step required
- Will ask before pulling in external libraries

## License

MIT
