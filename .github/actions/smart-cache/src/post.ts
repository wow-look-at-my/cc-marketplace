import * as core from '@actions/core';
import * as cache from '@actions/cache';
import * as exec from '@actions/exec';

async function run(): Promise<void> {
	try {
		const pathsJson = core.getState('paths');
		const key = core.getState('key');

		if (!pathsJson || !key) {
			core.info('No cache state found, skipping save');
			return;
		}

		const paths: string[] = JSON.parse(pathsJson);

		// Check for changes
		core.info('Checking for cache changes...');
		let lineCount = 0;
		const outputLines: string[] = [];

		await exec.exec('marketplace-build', ['cache-changed', ...paths], {
			listeners: {
				stdout: (data: Buffer) => {
					const lines = data.toString().split('\n').filter(Boolean);
					for (const line of lines) {
						lineCount++;
						if (lineCount <= 100) {
							outputLines.push(line);
						}
					}
				}
			}
		});

		if (lineCount === 0) {
			core.info('No cache changes detected, skipping save');
			return;
		}

		// Print changes (first 100)
		for (const line of outputLines) {
			core.info(line);
		}
		core.info(`Total: ${lineCount} files changed`);

		// Save cache
		core.info('Saving cache...');
		await cache.saveCache(paths, key);
		core.info('Cache saved');
	} catch (error) {
		if (error instanceof Error) {
			core.warning(`Cache save failed: ${error.message}`);
		}
	}
}

run();
