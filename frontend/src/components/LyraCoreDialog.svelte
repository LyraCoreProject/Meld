<script lang="ts">
	import { onMount } from 'svelte'
	import Modal from './Modal.svelte'
	import { setStatus, ui } from '../lib/state/editor.svelte'
	import { LyraCore, SetLyraCore } from '../../wailsjs/go/main/App'

	const close = () => (ui.dialog = null)

	// The LyraCore realm whose SpacetimeDB spawns are read from instead of a VMaNGOS world database.
	// The saved token is never sent here, so an empty token field keeps it.
	let url = $state('')
	let token = $state('')
	let hasToken = $state(false)
	let database = $state('')
	let shards = $state('')
	let busy = $state(false)
	let error = $state('')

	onMount(async () => {
		try {
			const info = await LyraCore()
			url = info.url
			hasToken = info.hasToken
			database = info.database
			shards = info.shards
		} catch {
			// No desktop app behind the page: start empty.
		}
	})

	async function save(nextUrl: string) {
		busy = true
		error = ''
		try {
			await SetLyraCore(nextUrl, token, database, shards)
			setStatus(nextUrl ? `Connected to the LyraCore realm at ${nextUrl}. Reopen a region to load its spawns.` : 'Cleared the LyraCore realm.')
			close()
		} catch (e) {
			error = String(e)
		} finally {
			busy = false
		}
	}
</script>

<Modal title="LyraCore realm" width={480} onclose={close}>
	<p>With a realm set, spawns are read from and saved to its SpacetimeDB instead of a VMaNGOS world database.</p>
	<p>The database and shard map are the gateway's <code>LYRACORE_DATABASE</code> and <code>LYRACORE_SHARD_MAP</code>.</p>
	<p class="warn">The token is a database owner's, saved in plain text in your Meld config file.</p>
	<div class="fields">
		<label for="lyra-url">SpacetimeDB URL</label>
		<input id="lyra-url" type="text" autocomplete="off" placeholder="http://127.0.0.1:3000" bind:value={url} />
		<label for="lyra-token">Owner token</label>
		<input id="lyra-token" type="password" autocomplete="off" bind:value={token} placeholder={hasToken ? 'Saved (leave blank to keep it)' : ''} />
		<label for="lyra-db">Database</label>
		<input id="lyra-db" type="text" autocomplete="off" placeholder="lyracore" bind:value={database} />
		<label for="lyra-shards">Shard map</label>
		<textarea id="lyra-shards" rows="3" placeholder="1:*=lyracore-world-1, 36:*=lyracore-instances" bind:value={shards}></textarea>
	</div>
	{#if error}<p class="error">{error}</p>{/if}
	{#snippet actions()}
		<button disabled={busy} onclick={() => save('')}>Clear realm</button>
		<button onclick={close}>Cancel</button>
		<button class="active" disabled={busy || url.trim() === ''} onclick={() => save(url.trim())}>{busy ? 'Connecting…' : 'Connect'}</button>
	{/snippet}
</Modal>

<style>
	.fields {
		display: grid;
		grid-template-columns: 120px 1fr;
		gap: 8px;
		align-items: center;
		margin-top: 10px;
	}
	.fields input,
	.fields textarea {
		width: 100%;
	}
	.warn {
		color: var(--warn, #d9a441);
	}
	.error {
		color: var(--danger, #e06c6c);
		white-space: pre-wrap;
	}
</style>
