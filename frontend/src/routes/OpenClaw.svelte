<script lang="ts">
	import { onMount } from 'svelte'
	import { appState } from '$lib/store'

	let loading = $state(true)
	let error = $state<string | null>(null)
	let status = $state<any>(null)
	let patterns = $state<any[]>([])
	let analyses = $state<any[]>([])

	onMount(async () => {
		await loadStatus()
		await loadPatterns()
		await loadAnalyses()
	})

	async function loadStatus() {
		try {
			loading = true
			const res = await fetch('/api/openclaw/status')
			if (!res.ok) throw new Error('Failed to load status')
			status = await res.json()
			error = null
		} catch (e) {
			error = e instanceof Error ? e.message : 'Unknown error'
		} finally {
			loading = false
		}
	}

	async function loadPatterns() {
		try {
			const res = await fetch('/api/openclaw/patterns?limit=20')
			if (!res.ok) throw new Error('Failed to load patterns')
			const data = await res.json()
			patterns = data.patterns || []
		} catch (e) {
			console.error('Failed to load patterns:', e)
		}
	}

	async function loadAnalyses() {
		try {
			const res = await fetch('/api/openclaw/analyses?limit=10')
			if (!res.ok) throw new Error('Failed to load analyses')
			const data = await res.json()
			analyses = data.analyses || []
		} catch (e) {
			console.error('Failed to load analyses:', e)
		}
	}

	async function triggerAnalysis() {
		try {
			const res = await fetch('/api/openclaw/trigger', { method: 'POST' })
			if (!res.ok) throw new Error('Failed to trigger analysis')
			await new Promise(resolve => setTimeout(resolve, 2000))
			await Promise.all([loadStatus(), loadPatterns(), loadAnalyses()])
		} catch (e) {
			error = e instanceof Error ? e.message : 'Unknown error'
		}
	}

	function formatDate(dateStr: string) {
		return new Date(dateStr).toLocaleString()
	}

	function getConfidenceColor(confidence: number) {
		if (confidence >= 0.8) return '#10b981'
		if (confidence >= 0.6) return '#f59e0b'
		return '#ef4444'
	}

	function getPatternTypeIcon(type: string) {
		switch (type) {
			case 'quality': return '🎯'
			case 'performance': return '⚡'
			case 'workflow': return '🔄'
			case 'success': return '✅'
			case 'agent': return '🤖'
			default: return '📊'
		}
	}
</script>

<div class="openclaw">
	<header>
		<div class="header-left">
			<h1>OpenClaw - AI Coach</h1>
			<p class="subtitle">Autonomous pattern detection & improvement suggestions</p>
		</div>
		<div class="controls">
			<button class="refresh-btn" onclick={loadStatus} disabled={loading}>
				{loading ? 'Loading...' : '↻ Refresh'}
			</button>
			<button class="trigger-btn" onclick={triggerAnalysis} disabled={loading}>
				▶ Run Analysis
			</button>
		</div>
	</header>

	{#if error}
		<div class="error-banner">
			<span class="error-icon">⚠</span>
			<span>{error}</span>
		</div>
	{/if}

	{#if loading && !status}
		<div class="loading">Loading OpenClaw status...</div>
	{:else if status}
		<div class="dashboard">
			<!-- Status Overview -->
			<section class="status-section">
				<h2>Status</h2>
				<div class="status-grid">
					<div class="status-card">
						<div class="status-label">Enabled</div>
						<div class="status-value">{status.enabled ? '✓ Yes' : '✗ No'}</div>
					</div>
					<div class="status-card">
						<div class="status-label">Patterns Detected</div>
						<div class="status-value">{status.state?.patterns_detected || 0}</div>
					</div>
					<div class="status-card">
						<div class="status-label">Proposals Generated</div>
						<div class="status-value">{status.state?.proposals_generated || 0}</div>
					</div>
					<div class="status-card">
						<div class="status-label">Acceptance Rate</div>
						<div class="status-value">{((status.state?.acceptance_rate || 0) * 100).toFixed(1)}%</div>
					</div>
				</div>
			</section>

			<!-- Recent Patterns -->
			<section class="patterns-section">
				<h2>Recent Patterns ({patterns.length})</h2>
				{#if patterns.length === 0}
					<div class="empty-state">No patterns detected yet</div>
				{:else}
					<div class="patterns-list">
						{#each patterns as pattern}
							<div class="pattern-card">
								<div class="pattern-header">
									<span class="pattern-icon">{getPatternTypeIcon(pattern.pattern_type)}</span>
									<span class="pattern-name">{pattern.pattern_name}</span>
									<span class="pattern-type">{pattern.pattern_type}</span>
								</div>
								<div class="pattern-description">{pattern.description}</div>
								<div class="pattern-meta">
									<span class="confidence" style="color: {getConfidenceColor(pattern.confidence)}">
										{(pattern.confidence * 100).toFixed(0)}% confidence
									</span>
									<span class="frequency">• {pattern.frequency} occurrences</span>
									<span class="date">• {formatDate(pattern.last_seen)}</span>
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</section>

			<!-- Analysis History -->
			<section class="analyses-section">
				<h2>Analysis History ({analyses.length})</h2>
				{#if analyses.length === 0}
					<div class="empty-state">No analyses run yet</div>
				{:else}
					<div class="analyses-list">
						{#each analyses as analysis}
							<div class="analysis-card">
								<div class="analysis-header">
									<span class="analysis-type">{analysis.analysis_type}</span>
									<span class="analysis-date">{formatDate(analysis.created_at)}</span>
								</div>
								<div class="analysis-stats">
									<span>🔍 {analysis.patterns_found} patterns</span>
									<span>💡 {analysis.proposals_created} proposals</span>
									<span>⏱ {analysis.execution_time_ms}ms</span>
								</div>
								<div class="analysis-scope">Scope: {analysis.scope}</div>
							</div>
						{/each}
					</div>
				{/if}
			</section>
		</div>
	{/if}
</div>

<style>
	.openclaw {
		padding: 2rem;
		max-width: 1400px;
		margin: 0 auto;
	}

	header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 2rem;
		padding-bottom: 1rem;
		border-bottom: 1px solid #333;
	}

	.header-left h1 {
		font-size: 2rem;
		margin: 0;
		color: #58a6ff;
	}

	.subtitle {
		color: #8b949e;
		margin-top: 0.5rem;
	}

	.controls {
		display: flex;
		gap: 1rem;
	}

	.refresh-btn, .trigger-btn {
		padding: 0.75rem 1.5rem;
		border-radius: 6px;
		border: 1px solid #30363d;
		background: #21262d;
		color: #c9d1d9;
		cursor: pointer;
		font-size: 0.9rem;
		transition: all 0.15s;
	}

	.refresh-btn:hover, .trigger-btn:hover {
		background: #30363d;
		border-color: #8b949e;
	}

	.trigger-btn {
		background: #238636;
		border-color: #238636;
		color: #fff;
	}

	.trigger-btn:hover {
		background: #2ea043;
	}

	.refresh-btn:disabled, .trigger-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.error-banner {
		background: #2d1f1f;
		border: 1px solid #f85149;
		border-radius: 8px;
		padding: 1rem;
		margin-bottom: 2rem;
		color: #f85149;
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.loading {
		text-align: center;
		padding: 3rem;
		color: #8b949e;
	}

	.dashboard {
		display: grid;
		gap: 2rem;
	}

	section {
		background: #161b22;
		border: 1px solid #30363d;
		border-radius: 8px;
		padding: 1.5rem;
	}

	section h2 {
		margin: 0 0 1.5rem 0;
		font-size: 1.5rem;
		color: #58a6ff;
	}

	/* Status Section */
	.status-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 1rem;
	}

	.status-card {
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 1.5rem;
		text-align: center;
	}

	.status-label {
		font-size: 0.875rem;
		color: #8b949e;
		margin-bottom: 0.5rem;
	}

	.status-value {
		font-size: 1.75rem;
		font-weight: 600;
		color: #c9d1d9;
	}

	/* Patterns Section */
	.patterns-list {
		display: grid;
		gap: 1rem;
	}

	.pattern-card {
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 1.25rem;
		transition: all 0.15s;
	}

	.pattern-card:hover {
		border-color: #58a6ff;
	}

	.pattern-header {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		margin-bottom: 0.75rem;
	}

	.pattern-icon {
		font-size: 1.5rem;
	}

	.pattern-name {
		font-size: 1.125rem;
		font-weight: 600;
		color: #c9d1d9;
		flex: 1;
	}

	.pattern-type {
		font-size: 0.75rem;
		padding: 0.25rem 0.75rem;
		background: #30363d;
		border-radius: 12px;
		color: #8b949e;
		text-transform: uppercase;
	}

	.pattern-description {
		color: #8b949e;
		margin-bottom: 0.75rem;
		line-height: 1.5;
	}

	.pattern-meta {
		font-size: 0.875rem;
		color: #8b949e;
		display: flex;
		gap: 0.5rem;
		align-items: center;
	}

	.confidence {
		font-weight: 600;
	}

	/* Analyses Section */
	.analyses-list {
		display: grid;
		gap: 1rem;
	}

	.analysis-card {
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 1rem;
	}

	.analysis-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 0.75rem;
	}

	.analysis-type {
		font-weight: 600;
		color: #58a6ff;
		text-transform: uppercase;
		font-size: 0.875rem;
	}

	.analysis-date {
		font-size: 0.875rem;
		color: #8b949e;
	}

	.analysis-stats {
		display: flex;
		gap: 1.5rem;
		margin-bottom: 0.5rem;
		color: #c9d1d9;
	}

	.analysis-scope {
		font-size: 0.875rem;
		color: #8b949e;
	}

	.empty-state {
		text-align: center;
		padding: 2rem;
		color: #8b949e;
	}
</style>
