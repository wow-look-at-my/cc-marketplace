import type { On } from 'claude-code'
import { expect, test } from 'claude-code/testing'

const CWD = '/work'
const NOTE = 'Your Bash command was a file read.'

// stage stands in for the engine and for slopfix beneath the module. plans
// maps a command to what read-plan would print. It records what each layer
// was asked.
function stage(on: On, plans: Record<string, object>) {
	const asked: unknown[] = []
	const reads: unknown[] = []
	const ran: string[] = []
	on('session.cwd', () => ({ value: CWD }))
	on('process.run', (_$, e) => {
		const stdin = JSON.parse(e.init?.stdin ?? '{}')
		asked.push({ argv: e.argv, stdin })
		const cmd = stdin.command as string
		const out = plans[cmd] ?? { reads: null, note: '' }
		return { value: { exitCode: 0, stdout: JSON.stringify(out), stderr: '' } } as never
	})
	on('tool.call', { tool: 'Read' }, (_$, e) => {
		reads.push({ file_path: e.file_path, offset: e.offset, limit: e.limit })
		return { result: { type: 'text' }, text: `READ ${e.file_path}` } as never
	})
	on('tool.call', { tool: 'Bash' }, (_$, e) => {
		ran.push(e.command)
		return { result: { stdout: 'bash ran', stderr: '', interrupted: false } } as never
	})
	return { asked, reads, ran }
}

test('a mapped read runs Read and carries the note', async ($, on) => {
	const { asked, reads, ran } = stage(on, {
		'sed -n 2,3p a.txt': { reads: [{ file_path: '/work/a.txt', offset: 2, limit: 2 }], note: NOTE },
	})
	const out = await $.tool.call({ tool: 'Bash', command: 'sed -n 2,3p a.txt' })
	expect((asked[0] as { argv: string[] }).argv.slice(1)).toEqual(['check', 'read-plan'])
	expect((asked[0] as { argv: string[] }).argv[0]).toContain('/bin/slopfix.ape')
	expect((asked[0] as { stdin: object }).stdin).toEqual({ command: 'sed -n 2,3p a.txt', cwd: CWD })
	expect(reads).toEqual([{ file_path: '/work/a.txt', offset: 2, limit: 2 }])
	expect(ran).toEqual([])
	expect(out.result).toEqual({ stdout: 'READ /work/a.txt', stderr: '', interrupted: false })
	expect(out.context).toEqual([NOTE])
})

test('several reads share the one Bash result, each under a header', async ($, on) => {
	const { reads } = stage(on, {
		'cat a.txt b.txt': { reads: [{ file_path: '/work/a.txt' }, { file_path: '/work/b.txt' }], note: NOTE },
	})
	const out = await $.tool.call({ tool: 'Bash', command: 'cat a.txt b.txt' })
	expect(reads.length).toBe(2)
	expect(out.result).toEqual({
		stdout: '==> /work/a.txt <==\nREAD /work/a.txt\n\n==> /work/b.txt <==\nREAD /work/b.txt',
		stderr: '',
		interrupted: false,
	})
})

test('a command slopfix does not map runs as Bash', async ($, on) => {
	const { reads, ran } = stage(on, {})
	await $.tool.call({ tool: 'Bash', command: 'cat a.txt | jq .x' })
	expect(reads).toEqual([])
	expect(ran).toEqual(['cat a.txt | jq .x'])
})

test('a failed Read runs the command as Bash', async ($, on) => {
	const ran: string[] = []
	on('session.cwd', () => ({ value: CWD }))
	on('process.run', () => ({
		value: { exitCode: 0, stdout: JSON.stringify({ reads: [{ file_path: '/work/x' }], note: NOTE }), stderr: '' },
	}) as never)
	on('tool.call', { tool: 'Read' }, () => ({ deny: 'no' }))
	on('tool.call', { tool: 'Bash' }, (_$, e) => {
		ran.push(e.command)
		return { result: { stdout: 'bash ran', stderr: '', interrupted: false } } as never
	})
	const out = await $.tool.call({ tool: 'Bash', command: 'cat x' })
	expect(ran).toEqual(['cat x'])
	expect(out.result).toEqual({ stdout: 'bash ran', stderr: '', interrupted: false })
})

test('a slopfix that fails is reported, and the command runs as Bash', async ($, on) => {
	const ran: string[] = []
	on('session.cwd', () => ({ value: CWD }))
	on('process.run', () => ({ value: { exitCode: 2, stdout: '', stderr: 'unknown command "read-plan"' } }) as never)
	on('tool.call', { tool: 'Bash' }, (_$, e) => {
		ran.push(e.command)
		return { result: { stdout: 'bash ran', stderr: '', interrupted: false } } as never
	})
	await $.tool.call({ tool: 'Bash', command: 'cat x' })
	expect(ran).toEqual(['cat x'])
})
