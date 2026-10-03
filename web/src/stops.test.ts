import { test } from 'node:test'
import assert from 'node:assert/strict'
import { groupStops, relevantGroups } from './stops.ts'

const stop = (id: string, name: string, walk_s: number, lines: string[], station?: string, station_id?: string) =>
  ({ id, name, walk_s, lines, station, station_id, lat: 0, lon: 0 })

test('platforms of a station collapse into one row, nearest first', () => {
  const g = groupStops([
    stop('p1', 'Central Station, Platform 1', 1200, ['train T1'], 'Central Station', 'H'),
    stop('b1', 'Example St', 600, ['bus 999'], 'Example St', 'G1'),
    stop('p2', 'Central Station, Platform 2', 1150, ['train T9', 'train T1'], 'Central Station', 'H'),
  ])
  assert.deepEqual(g.map((x) => x.name), ['Example St', 'Central Station'])
  assert.deepEqual(g[1].ids, ['p1', 'p2'])
  assert.deepEqual(g[1].lines, ['train T1', 'train T9'])
  assert.equal(g[1].walk_s, 1150)
})

test('default list keeps the nearest few per line and all stations', () => {
  const groups = groupStops([
    ...Array.from({ length: 10 }, (_, i) => stop(`b${i}`, `Bus stop ${i}`, 600 + i * 30, ['bus 999'])),
    stop('p1', 'Central Station, Platform 1', 1200, ['train T1'], 'Central Station', 'H'),
  ])
  const shown = relevantGroups(groups).map((g) => g.name)
  assert.deepEqual(shown, ['Bus stop 0', 'Bus stop 1', 'Bus stop 2', 'Central Station'])
})
