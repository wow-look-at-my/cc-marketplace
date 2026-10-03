// A Read call that prints what one plain Bash file read prints.
export type ReadPlan = {
	path: string
	offset?: number
	limit?: number
	fromEnd?: number
}

const DEFAULT_LINES = 10
const COUNT = /^\+?[0-9]+$/
const SED_RANGE = /^([0-9]+)(?:,([0-9]+))?p$/

// words splits a command that is one plain call: words and quotes only. Any
// shell syntax past that changes what runs, so the answer is null.
export function words(command: string): string[] | null {
	const out: string[] = []
	let cur = ''
	let started = false
	let quote = ''
	for (const ch of command.trim()) {
		if (quote) {
			if (ch === quote) quote = ''
			else if (quote === '"' && /[$`\\]/.test(ch)) return null
			else cur += ch
			continue
		}
		if (ch === "'" || ch === '"') {
			quote = ch
			started = true
		} else if (ch === ' ' || ch === '\t') {
			if (started) out.push(cur)
			cur = ''
			started = false
		} else if (/[|&;<>$`(){}*?[\]\\\n#~!]/.test(ch)) {
			return null
		} else {
			cur += ch
			started = true
		}
	}
	if (quote) return null
	if (started) out.push(cur)
	return out
}

// plan maps `cat`, `head`, `tail` and `sed -n` onto Read. Each spelling it
// cannot map exactly returns null, and the Bash call then runs as written.
export function plan(command: string): ReadPlan | null {
	const w = words(command)
	if (!w || w.length < 2) return null
	const [name, ...args] = w
	switch (name) {
		case 'cat':
			return cat(args)
		case 'head':
		case 'tail':
			return headTail(name === 'tail', args)
		case 'sed':
			return sed(args)
	}
	return null
}

function cat(args: string[]): ReadPlan | null {
	const files = args.filter(a => a !== '-n' && a !== '--number')
	if (files.length !== 1 || files[0].startsWith('-')) return null
	return { path: files[0] }
}

function headTail(tail: boolean, args: string[]): ReadPlan | null {
	let count = String(DEFAULT_LINES)
	let file = ''
	for (let i = 0; i < args.length; i++) {
		const a = args[i]
		const m = /^-n(\+?[0-9]+)$/.exec(a) ?? /^--lines=(\+?[0-9]+)$/.exec(a) ?? /^-([0-9]+)$/.exec(a)
		if ((a === '-n' || a === '--lines') && i + 1 < args.length) count = args[++i]
		else if (m) count = m[1]
		else if (a.startsWith('-') || file) return null
		else file = a
	}
	if (!file || !COUNT.test(count)) return null
	const n = Number(count.replace('+', ''))
	const fromStart = count.startsWith('+')
	if (n === 0) return null
	if (!tail) return fromStart ? null : { path: file, limit: n }
	return fromStart ? { path: file, offset: n } : { path: file, fromEnd: n }
}

function sed(args: string[]): ReadPlan | null {
	let quiet = false
	const rest: string[] = []
	for (const a of args) {
		if (a === '-n' || a === '--quiet' || a === '--silent') quiet = true
		else if (a.startsWith('-')) return null
		else rest.push(a)
	}
	const m = rest.length === 2 ? SED_RANGE.exec(rest[0]) : null
	if (!quiet || !m) return null
	const a = Number(m[1])
	const b = m[2] === undefined ? a : Number(m[2])
	if (a === 0 || b < a) return null
	return { path: rest[1], offset: a, limit: b - a + 1 }
}

// absolute joins a relative path onto dir. Read takes absolute paths only.
export function absolute(path: string, dir: string): string {
	const parts = (path.startsWith('/') ? path : `${dir}/${path}`).split('/')
	const out: string[] = []
	for (const p of parts) {
		if (p === '' || p === '.') continue
		if (p === '..') out.pop()
		else out.push(p)
	}
	return `/${out.join('/')}`
}

// lineCount counts lines the way Read numbers them: a last line with no
// newline still counts.
export function lineCount(text: string): number {
	if (text === '') return 0
	const n = text.split('\n').length - 1
	return text.endsWith('\n') ? n : n + 1
}
