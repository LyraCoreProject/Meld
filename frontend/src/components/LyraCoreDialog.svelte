<script lang="ts">
	import { onMount } from 'svelte'
	import Modal from './Modal.svelte'
	import { setStatus, ui } from '../lib/state/editor.svelte'
	import { LyraCore, SetLyraCore } from '../../wailsjs/go/main/App'
	import { setRealmSaves } from '../lib/state/saving'

	const close = () => (ui.dialog = null)

	// The LyraCore realm whose SpacetimeDB spawns are read from instead of a VMaNGOS world database.
	// The saved token is never sent here, so an empty token field keeps it.
	let url = $state('')
	let token = $state('')
	let hasToken = $state(false)
	let database = $state('')
	// Set by `lyracore editor` for this session, so it can't be changed here.
	let session = $state(false)
	let busy = $state(false)
	let error = $state('')

	onMount(async () => {
		try {
			const info = await LyraCore()
			url = info.url
			hasToken = info.hasToken
			database = info.database
			session = info.session
		} catch {
			// No desktop app behind the page: start empty.
		}
	})

	async function save(nextUrl: string) {
		busy = true
		error = ''
		try {
			await SetLyraCore(nextUrl, token, database)
			setRealmSaves(nextUrl !== '')
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
	{#if session}
		<p><code>lyracore editor</code> connected this session to the local realm. Saved spawns show in it at once.</p>
	{:else}
		<p class="warn">The token is the realm's Operator's (on a local realm, <code>.lyracore/native-owner-token</code>), saved in plain text in your Meld config file.</p>
	{/if}
	<div class="fields">
		<label for="lyra-url">SpacetimeDB URL</label>
		<input id="lyra-url" type="text" autocomplete="off" placeholder="http://127.0.0.1:3000" disabled={session} bind:value={url} />
		<label for="lyra-token">Operator token</label>
		<input id="lyra-token" type="password" autocomplete="off" disabled={session} bind:value={token} placeholder={hasToken ? 'Saved (leave blank to keep it)' : ''} />
		<label for="lyra-db">Database</label>
		<input id="lyra-db" type="text" autocomplete="off" placeholder="lyracore" disabled={session} bind:value={database} />
	</div>
	{#if error}<p class="error">{error}</p>{/if}
	{#snippet actions()}
		{#if session}
			<button class="active" onclick={close}>Close</button>
		{:else}
			<button disabled={busy} onclick={() => save('')}>Clear realm</button>
			<button onclick={close}>Cancel</button>
			<button class="active" disabled={busy || url.trim() === ''} onclick={() => save(url.trim())}>{busy ? 'Connecting…' : 'Connect'}</button>
		{/if}
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
	.fields input {
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
