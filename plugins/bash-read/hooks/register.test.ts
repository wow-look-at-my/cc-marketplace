import type { On } from 'claude-code'
import { expect, test } from 'claude-code/testing'

const CWD = '/work'
const FILE = Array.from({ length: 30 }, (_, i) => `line ${i + 1}`).join('\n') + '\n'

// stage stands in for the engine beneath the plugin. It records each Read
// input, and answers Bash only when the plugin lets the command run.
function stage(on: On) {
	const reads: unknown[] = []
	const ran: string[] = []
	on('session.cwd', () => ({ value: CWD }))
	on('fs.read', () => ({ value: FILE }))
	on('tool.call', { tool: 'Read' }, (_$, e) => {
		reads.push({ file_path: e.file_path, offset: e.offset, limit: e.limit })
		return { result: { type: 'text' }, text: `READ ${e.file_path}` } as never
	})
	on('tool.call', { tool: 'Bash' }, (_$, e) => {
		ran.push(e.command)
		return { result: { stdout: 'bash ran', stderr: '', interrupted: false } } as never
	})
	return { reads, ran }
}

const mapped: [string, object][] = [
	['cat f.txt', { file_path: '/work/f.txt' }],
	['cat -n /abs/f.txt', { file_path: '/abs/f.txt' }],
	['head f.txt', { file_path: '/work/f.txt', limit: 10 }],
	['head -n 5 sub/f.txt', { file_path: '/work/sub/f.txt', limit: 5 }],
	['head -n5 f.txt', { file_path: '/work/f.txt', limit: 5 }],
	['head -60 f.txt', { file_path: '/work/f.txt', limit: 60 }],
	['head --lines=7 ../f.txt', { file_path: '/f.txt', limit: 7 }],
	['tail -n 5 f.txt', { file_path: '/work/f.txt', offset: 26, limit: 5 }],
	['tail f.txt', { file_path: '/work/f.txt', offset: 21, limit: 10 }],
	['tail -100 f.txt', { file_path: '/work/f.txt', offset: 1, limit: 30 }],
	['tail -n +12 f.txt', { file_path: '/work/f.txt', offset: 12 }],
	["sed -n '10,20p' f.txt", { file_path: '/work/f.txt', offset: 10, limit: 11 }],
	['sed -n 4p f.txt', { file_path: '/work/f.txt', offset: 4, limit: 1 }],
	['sed --quiet "2,3p" "my file.txt"', { file_path: '/work/my file.txt', offset: 2, limit: 2 }],
]

for (const [command, want] of mapped) {
	test(`${command} is answered by Read`, async ($, on) => {
		const { reads, ran } = stage(on)
		const out = await $.tool.call({ tool: 'Bash', command })
		expect(reads).toEqual([{ offset: undefined, limit: undefined, ...want }])
		expect(ran).toEqual([])
		expect(out.result).toEqual({ stdout: `READ ${(want as { file_path: string }).file_path}`, stderr: '', interrupted: false })
	})
}

const untouched = [
	'cat a.txt b.txt',
	'cat f.txt | jq .x',
	'cat f.txt > g.txt',
	'cd sub && cat f.txt',
	'cat $F',
	'cat *.txt',
	'cat ~/f.txt',
	'cat -A f.txt',
	'head -c 20 f.txt',
	'head -n 0 f.txt',
	'head -n -5 f.txt',
	'tail -f f.txt',
	"sed -n '/re/p' f.txt",
	"sed -n '5,2p' f.txt",
	'sed -n -e 3p f.txt',
	"sed 's/a/b/' f.txt",
	'ls -la',
]

for (const command of untouched) {
	test(`${command} runs as Bash`, async ($, on) => {
		const { reads, ran } = stage(on)
		await $.tool.call({ tool: 'Bash', command })
		expect(reads).toEqual([])
		expect(ran).toEqual([command])
	})
}

test('a Read that fails falls back to the Bash command', async ($, on) => {
	const ran: string[] = []
	on('session.cwd', () => ({ value: CWD }))
	on('tool.call', { tool: 'Read' }, () => ({ deny: 'no' }))
	on('tool.call', { tool: 'Bash' }, (_$, e) => {
		ran.push(e.command)
		return { result: { stdout: 'bash ran', stderr: '', interrupted: false } } as never
	})
	const out = await $.tool.call({ tool: 'Bash', command: 'cat missing.txt' })
	expect(ran).toEqual(['cat missing.txt'])
	expect(out.result).toEqual({ stdout: 'bash ran', stderr: '', interrupted: false })
})
