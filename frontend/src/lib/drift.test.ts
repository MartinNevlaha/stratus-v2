import { describe, it, expect } from 'vitest'
import { plannedPaths, outOfScopeFiles } from './drift'

describe('plannedPaths', () => {
  it('should collect file-like paths mentioned in the plan', () => {
    const plan = 'Edit `api/routes.go` and frontend/src/App.svelte; add db/schema.go. See https://example.com/a.html'
    expect(plannedPaths(plan).sort()).toEqual(['api/routes.go', 'db/schema.go', 'frontend/src/App.svelte'])
  })

  it('should also collect bare file names with a known extension', () => {
    const plan = 'Create i.txt containing "red", then k.txt; update README.md. Note e.g. this and v1.2 are not files.'
    expect(plannedPaths(plan).sort()).toEqual(['README.md', 'i.txt', 'k.txt'])
  })

  it('should return an empty list for an empty or path-free plan', () => {
    expect(plannedPaths('')).toEqual([])
    expect(plannedPaths('Just refactor things')).toEqual([])
  })
})

describe('outOfScopeFiles', () => {
  const planned = ['api/routes.go', 'db/schema.go']

  it('should accept files whose path ends with a planned path', () => {
    expect(outOfScopeFiles(['/repo/api/routes.go', '/repo/db/schema.go'], planned)).toEqual([])
  })

  it('should match a bare planned file name by basename', () => {
    expect(outOfScopeFiles(['/tmp/proj/i.txt', '/tmp/proj/other.txt'], ['i.txt'])).toEqual(['/tmp/proj/other.txt'])
  })

  it('should not treat a longer name as a match for a bare file name', () => {
    expect(outOfScopeFiles(['/tmp/proj/pi.txt'], ['i.txt'])).toEqual(['/tmp/proj/pi.txt'])
  })

  it('should list touched files that match no planned path', () => {
    expect(outOfScopeFiles(['/repo/api/routes.go', '/repo/cmd/main.go'], planned)).toEqual(['/repo/cmd/main.go'])
  })

  it('should not flag anything when the plan names no files', () => {
    expect(outOfScopeFiles(['/repo/x.go'], [])).toEqual([])
  })

  it('should ignore docs/plans files the workflow itself writes', () => {
    expect(outOfScopeFiles(['/repo/docs/plans/spec-x-plan.md'], planned)).toEqual([])
  })
})
