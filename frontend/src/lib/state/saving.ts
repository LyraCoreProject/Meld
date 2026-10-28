import { ui } from './editor.svelte'

// The Saving bar's state (#343): every save (the map, a compile, the spawns, the database editors'
// Save buttons) runs through these, so one bar in the middle of the window covers all of them.
// Saves may overlap (an editor's Save while a publish runs): each holds a place in a list, and the
// bar names the newest one still running. Set at the start and end of a save only, never per frame.

const running: { label: string; detail: string; now: boolean }[] = []

// Whether a LyraCore realm is set: the database editors' and the spawns' saves then update the
// running realm, which shows them at once, so the bar says so and shows without the delay.
let realm = false

/** Tells the saves whether a LyraCore realm is set (`App.svelte` at start, the LyraCore Realm dialog). */
export function setRealmSaves(on: boolean) {
	realm = on
}

function show() {
	const top = running[running.length - 1]
	ui.saving = top ? top.label : ''
	ui.savingDetail = top ? top.detail : ''
	ui.savingNow = top ? top.now : false
}

/** Shows `label` ("Saving map") on the Saving bar until the returned function is called (once). */
export function startSaving(label: string): () => void {
	return begin({ label, detail: '', now: false })
}

/**
 * `startSaving` for a save that goes to the LyraCore realm when one is set: the bar then reads
 * "Updating the realm" over `label` and shows at once. Without a realm it is `startSaving(label)`.
 */
export function startRealmSaving(label: string): () => void {
	if (!realm) return startSaving(label)
	return begin({ label: 'Updating the realm', detail: label, now: true })
}

function begin(entry: { label: string; detail: string; now: boolean }): () => void {
	running.push(entry)
	show()
	let ended = false
	return () => {
		if (ended) return
		ended = true
		const i = running.indexOf(entry)
		if (i >= 0) running.splice(i, 1)
		show()
	}
}

/** `withSaving` through `startRealmSaving`. */
export async function withRealmSaving<T>(label: string, fn: () => Promise<T>): Promise<T> {
	const end = startRealmSaving(label)
	try {
		return await fn()
	} finally {
		end()
	}
}

/** Runs `fn` with `label` on the Saving bar, clearing it when `fn` ends, whether or not it threw. */
export async function withSaving<T>(label: string, fn: () => Promise<T>): Promise<T> {
	const end = startSaving(label)
	try {
		return await fn()
	} finally {
		end()
	}
}

/** Sets the progress line under the bar (a publish's log) for the newest save running; it goes with that save. */
export function savingDetail(line: string) {
	const top = running[running.length - 1]
	if (!top) return
	top.detail = line
	ui.savingDetail = line
}
