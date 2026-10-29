import { expect, test } from 'bun:test'
import { benillaLabel } from '../src/lib/io/benillaLabel'

test('says what stops Benilla from starting', () => {
	const b = { path: '/b', built: false, building: false, error: '' }
	expect(benillaLabel({ ...b, path: '' })).toBe('Benilla (no folder chosen)')
	expect(benillaLabel(b)).toBe('Benilla (not built)')
	expect(benillaLabel({ ...b, error: 'exit status 101' })).toBe('Benilla (build failed)')
	expect(benillaLabel({ ...b, building: true })).toBe('Benilla (building…)')
	expect(benillaLabel({ ...b, built: true })).toBe('Benilla')
})
