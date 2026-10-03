import type { Engine, Register } from 'claude-code'
import { absolute, lineCount, note, plan, readCall, type ReadPlan } from './parse.ts'

type Done = { call: string; path: string; text: string }

// readOne runs one planned Read as its own tool call, so permissions and Read
// hooks see it. Null means the Bash command runs as written instead.
async function readOne($: Engine, p: ReadPlan, cwd: string): Promise<Done | null> {
	const path = absolute(p.path, cwd)
	let { offset, limit } = p
	if (p.fromEnd !== undefined) {
		let total: number
		try {
			total = lineCount(await $.fs.read(path))
		} catch {
			return null
		}
		offset = Math.max(1, total - p.fromEnd + 1)
		limit = Math.min(p.fromEnd, total) || undefined
	}
	const read = await $.tool.call({
		tool: 'Read',
		file_path: path,
		...(offset !== undefined && { offset }),
		...(limit !== undefined && { limit }),
	})
	if (read.deny !== undefined || read.isError || read.text === undefined) return null
	return { call: readCall(path, offset, limit), path, text: read.text }
}

export const register: Register = on => {
	// A Bash read the plan maps is answered by Read calls, one per file. The
	// model gets their text as the Bash result, and a names the calls. Anything
	// else, or any Read that fails, runs the Bash command.
	on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
		const plans = plan(e.command)
		if (!plans) return next(e)
		const cwd = await $.session.cwd()
		const done: Done[] = []
		for (const p of plans) {
			const d = await readOne($, p, cwd)
			if (!d) return next(e)
			done.push(d)
		}
		const stdout = done.length === 1 ? done[0].text : done.map(d => `==> ${d.path} <==\n${d.text}`).join('\n\n')
		return {
			result: { stdout, stderr: '', interrupted: false },
			context: [note(e.command, done.map(d => d.call))],
		}
	})
}
