import type { Register } from 'claude-code'
import { absolute, lineCount, plan } from './parse.ts'

export const register: Register = on => {
	// A Bash read the plan maps is answered by the Read tool, run as its own
	// call so permissions and Read hooks see it. The model gets Read's text as
	// the Bash result. Anything else, or a Read that fails, runs the Bash
	// command unchanged.
	on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
		const p = plan(e.command)
		if (!p) return next(e)
		const path = absolute(p.path, await $.session.cwd())
		let { offset, limit } = p
		if (p.fromEnd !== undefined) {
			let total: number
			try {
				total = lineCount(await $.fs.read(path))
			} catch {
				return next(e)
			}
			offset = Math.max(1, total - p.fromEnd + 1)
			limit = Math.min(p.fromEnd, total)
		}
		const read = await $.tool.call({
			tool: 'Read',
			file_path: path,
			...(offset !== undefined && { offset }),
			...(limit ? { limit } : {}),
		})
		if (read.deny !== undefined || read.isError || read.text === undefined) return next(e)
		return { result: { stdout: read.text, stderr: '', interrupted: false } }
	})
}
