/** The folders the start dialog (#689) can still ask for. */
export interface FolderGaps {
	client: boolean
	server: boolean
}

/**
 * Which folders are unset, and so whether to ask: not once the user ticked "Don't ask again". With a
 * LyraCore realm set, the realm holds the spawns and the database, so the server folder is not asked for.
 */
export function folderGaps(clientPath: string, serverPath: string, dontAsk: boolean, realm = false): FolderGaps | null {
	if (dontAsk) return null
	const gaps = { client: clientPath.trim() === '', server: !realm && serverPath.trim() === '' }
	return gaps.client || gaps.server ? gaps : null
}
