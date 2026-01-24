import * as core from '@actions/core';
import * as cache from '@actions/cache';
import * as exec from '@actions/exec';

async function run(): Promise<void> {
	try {
		const paths = core.getInput('path').split(/\s+/).filter(Boolean);
		const key = core.getInput('key');

		// Restore cache
		const cacheKey = await cache.restoreCache(paths, key);
		if (cacheKey) {
			core.info(`Cache restored from key: ${cacheKey}`);
			core.setOutput('cache-hit', 'true');
		} else {
			core.info('Cache not found');
			core.setOutput('cache-hit', 'false');
		}

		// Take snapshot
		core.info('Taking cache snapshot...');
		await exec.exec('marketplace-build', ['cache-snapshot', ...paths]);

		// Save state for post step
		core.saveState('paths', JSON.stringify(paths));
		core.saveState('key', key);
	} catch (error) {
		if (error instanceof Error) {
			core.setFailed(error.message);
		}
	}
}

run();
