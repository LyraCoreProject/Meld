/** Benilla's entry under Game › Game client, with what stops it from starting. */
export function benillaLabel(b: { path: string; built: boolean; building: boolean; error: string }): string {
	if (b.building) return 'Benilla (building…)'
	if (!b.path) return 'Benilla (no folder chosen)'
	if (!b.built) return b.error ? 'Benilla (build failed)' : 'Benilla (not built)'
	return 'Benilla'
}
