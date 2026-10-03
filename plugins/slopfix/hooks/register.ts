import type { Engine, Register } from 'claude-code'

type ReadArgs = { file_path: string; offset?: number; limit?: number }
type ReadPlan = { reads: ReadArgs[] | null; note: string }

// plan asks slopfix how a Bash command maps onto Read. slopfix owns the
// mapping and the note. This module only runs the calls.
async function plan($: Engine, command: string): Promise<ReadPlan> {
	const run = await $.process.run([`${$.plugin.root}/bin/slopfix.ape`, 'check', 'read-plan'], {
		stdin: JSON.stringify({ command, cwd: await $.session.cwd() }),
	})
	if (run.exitCode !== 0) throw new Error(`slopfix read-plan exited ${run.exitCode}: ${run.stderr.trim()}`)
	return JSON.parse(run.stdout) as ReadPlan
}

export const register: Register = on => {
	// A Bash file read that slopfix maps is answered by real Read calls, one per
	// file, and the model gets a names them. Anything else, or any Read that
	// fails, runs the Bash command as written. A slopfix that cannot answer
	// throws, so the engine reports the skip and Bash runs.
	on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
		const p = await plan($, e.command)
		if (!p.reads?.length) return next(e)
		const texts: string[] = []
		for (const r of p.reads) {
			const read = await $.tool.call({ tool: 'Read', ...r })
			if (read.deny !== undefined || read.isError || read.text === undefined) return next(e)
			texts.push(read.text)
		}
		const stdout = texts.length === 1 ? texts[0] : texts.map((t, i) => `==> ${p.reads![i].file_path} <==\n${t}`).join('\n\n')
		return {
			result: { stdout, stderr: '', interrupted: false },
			context: [p.note],
		}
	})
}
