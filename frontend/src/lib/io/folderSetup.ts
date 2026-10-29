import { LyraCore, VmangosPath } from '../../../wailsjs/go/main/App'
import { prefs, ui, wow } from '../state/editor.svelte'
import { hasBackend } from './backend'
import { folderGaps } from './folderGaps'

/**
 * Opens the folder dialog at the start when the client or the server folder is unset and no other
 * dialog is up. A set LyraCore realm stands in for the server folder.
 */
export async function offerFolderSetup() {
	if (!hasBackend()) return
	const server = await VmangosPath().catch(() => 'set')
	const realm = await LyraCore()
		.then((info) => info.url !== '')
		.catch(() => false)
	if (ui.dialog !== null) return
	if (folderGaps(wow.path, server, prefs.skipFolderSetup, realm)) ui.dialog = 'folderSetup'
}
