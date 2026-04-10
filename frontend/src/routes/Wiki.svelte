<script lang="ts">
	import { listWikiPages, searchWiki, queryWiki, getWikiGraph, getVaultStatus, triggerVaultSync, triggerOnboarding, getOnboardingStatus } from '$lib/api'
	import type { WikiPage, WikiGraphData, WikiQueryResult, VaultStatus, OnboardingProgress, OnboardingResult } from '$lib/types'

	let pages = $state<WikiPage[]>([])
	let totalCount = $state(0)
	let searchResults = $state<WikiPage[]>([])
	let searchQuery = $state('')
	let selectedPage = $state<WikiPage | null>(null)
	let queryInput = $state('')
	let queryResult = $state<WikiQueryResult | null>(null)
	let queryLoading = $state(false)
	let queryError = $state<string | null>(null)
	let activeView = $state<'browse' | 'search' | 'query' | 'graph'>('browse')
	let typeFilter = $state('')
	let statusFilter = $state('')
	let graphData = $state<WikiGraphData | null>(null)
	let vaultStatus = $state<VaultStatus | null>(null)
	let loading = $state(false)
	let searchLoading = $state(false)
	let graphLoading = $state(false)
	let syncLoading = $state(false)
	let error = $state<string | null>(null)
	let syncMessage = $state<string | null>(null)

	let onboardingProgress = $state<OnboardingProgress | null>(null)
	let onboardingResult = $state<OnboardingResult | null>(null)
	let onboardingDepth = $state<'shallow' | 'standard' | 'deep'>('standard')
	let onboardingLoading = $state(false)
	let onboardingError = $state<string | null>(null)
	let showOnboarding = $state(false)

	$effect(() => {
		loadPages()
		loadVaultStatus()
	})

	async function loadPages() {
		loading = true
		error = null
		try {
			const data = await listWikiPages({
				type: typeFilter || undefined,
				status: statusFilter || undefined,
				limit: 100,
			})
			pages = data.pages
			totalCount = data.count
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load wiki pages'
		} finally {
			loading = false
		}
	}

	async function loadVaultStatus() {
		try {
			vaultStatus = await getVaultStatus()
		} catch (e) {
			console.error('Failed to load vault status:', e)
		}
	}

	async function doSearch() {
		if (!searchQuery.trim()) return
		searchLoading = true
		error = null
		try {
			const data = await searchWiki(searchQuery.trim(), typeFilter || undefined, 50)
			searchResults = data.results
		} catch (e) {
			error = e instanceof Error ? e.message : 'Search failed'
		} finally {
			searchLoading = false
		}
	}

	async function doQuery() {
		if (!queryInput.trim()) return
		queryLoading = true
		queryError = null
		queryResult = null
		try {
			queryResult = await queryWiki(queryInput.trim(), false, 10)
		} catch (e) {
			queryError = e instanceof Error ? e.message : 'Query failed'
		} finally {
			queryLoading = false
		}
	}

	async function loadGraph() {
		graphLoading = true
		error = null
		try {
			graphData = await getWikiGraph(undefined, 200)
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load graph'
		} finally {
			graphLoading = false
		}
	}

	async function doVaultSync() {
		syncLoading = true
		syncMessage = null
		try {
			const result = await triggerVaultSync()
			syncMessage = result.message || 'Sync triggered'
			await loadVaultStatus()
		} catch (e) {
			syncMessage = e instanceof Error ? e.message : 'Sync failed'
		} finally {
			syncLoading = false
		}
	}

	async function startOnboarding() {
		onboardingLoading = true
		onboardingError = null
		onboardingResult = null
		try {
			await triggerOnboarding({ depth: onboardingDepth })
			pollOnboardingStatus()
		} catch (e) {
			onboardingError = e instanceof Error ? e.message : 'Failed to start onboarding'
			onboardingLoading = false
		}
	}

	async function pollOnboardingStatus() {
		try {
			const status = await getOnboardingStatus()
			onboardingProgress = status
			if (status.result) {
				onboardingResult = status.result
			}
			if (status.status === 'complete' || status.status === 'failed') {
				onboardingLoading = false
				if (status.status === 'complete') {
					await loadPages() // refresh page list
				}
			} else {
				setTimeout(pollOnboardingStatus, 2000) // poll every 2s
			}
		} catch (e) {
			onboardingError = e instanceof Error ? e.message : 'Failed to check status'
			onboardingLoading = false
		}
	}

	function switchView(view: 'browse' | 'search' | 'query' | 'graph') {
		activeView = view
		if (view === 'graph' && !graphData) {
			loadGraph()
		}
	}

	function formatDate(ts: string) {
		if (!ts) return '—'
		return new Date(ts).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
	}

	function stalenessColor(score: number): string {
		if (score >= 0.8) return '#f85149'
		if (score >= 0.5) return '#d29922'
		if (score >= 0.2) return '#3fb950'
		return '#58a6ff'
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'published': return 'badge-published'
			case 'draft': return 'badge-draft'
			case 'stale': return 'badge-stale'
			case 'archived': return 'badge-archived'
			default: return 'badge-default'
		}
	}

	function typeBadgeClass(type: string): string {
		switch (type) {
			case 'summary': return 'type-summary'
			case 'entity': return 'type-entity'
			case 'concept': return 'type-concept'
			case 'answer': return 'type-answer'
			case 'index': return 'type-index'
			default: return 'type-default'
		}
	}

	// Graph layout: assign positions in a grid
	function graphNodePositions(nodes: WikiGraphData['nodes']): Map<string, { x: number; y: number }> {
		const positions = new Map<string, { x: number; y: number }>()
		const cols = Math.ceil(Math.sqrt(nodes.length))
		const cellW = 160
		const cellH = 100
		nodes.forEach((n, i) => {
			const col = i % cols
			const row = Math.floor(i / cols)
			positions.set(n.id, {
				x: 80 + col * cellW,
				y: 50 + row * cellH,
			})
		})
		return positions
	}

	let graphPositions = $derived(graphData ? graphNodePositions(graphData.nodes) : new Map())

	let graphWidth = $derived(
		graphData && graphData.nodes.length > 0
			? Math.ceil(Math.sqrt(graphData.nodes.length)) * 160 + 80
			: 800
	)
	let graphHeight = $derived(
		graphData && graphData.nodes.length > 0
			? Math.ceil(graphData.nodes.length / Math.ceil(Math.sqrt(graphData.nodes.length))) * 100 + 80
			: 400
	)
</script>

<div class="wiki">
	<header>
		<div class="header-left">
			<h1>Wiki</h1>
			<p class="subtitle">Project knowledge base — pages, search, and graph</p>
		</div>

		<div class="vault-status">
			{#if vaultStatus}
				<span class="vault-info">
					{vaultStatus.file_count} vault files
					{#if vaultStatus.last_sync}
						· synced {formatDate(vaultStatus.last_sync)}
					{/if}
				</span>
			{/if}
			<button class="onboard-btn" onclick={() => showOnboarding = !showOnboarding} disabled={onboardingLoading}>
				{onboardingLoading ? 'Onboarding...' : 'Onboard Project'}
			</button>
			<button class="sync-btn" onclick={doVaultSync} disabled={syncLoading}>
				{syncLoading ? 'Syncing...' : '↻ Sync Vault'}
			</button>
			{#if syncMessage}
				<span class="sync-msg">{syncMessage}</span>
			{/if}
		</div>
	</header>

	{#if showOnboarding}
		<div class="onboarding-panel">
			<div class="onboarding-header">
				<h3>Project Onboarding</h3>
				<p class="onboarding-desc">Auto-generate documentation wiki pages from your codebase.</p>
			</div>

			<div class="onboarding-controls">
				<label class="depth-label">
					Depth:
					<select bind:value={onboardingDepth} disabled={onboardingLoading}>
						<option value="shallow">Shallow (3-5 pages)</option>
						<option value="standard">Standard (8-15 pages)</option>
						<option value="deep">Deep (15-25 pages)</option>
					</select>
				</label>
				<button class="start-btn" onclick={startOnboarding} disabled={onboardingLoading}>
					{onboardingLoading ? 'Running...' : 'Start Onboarding'}
				</button>
			</div>

			{#if onboardingError}
				<div class="onboarding-error">{onboardingError}</div>
			{/if}

			{#if onboardingProgress && onboardingProgress.status !== 'idle'}
				<div class="onboarding-progress">
					<div class="progress-header">
						<span class="progress-status">{onboardingProgress.status}</span>
						{#if onboardingProgress.total > 0}
							<span class="progress-count">{onboardingProgress.generated}/{onboardingProgress.total}</span>
						{/if}
					</div>
					{#if onboardingProgress.current_page}
						<div class="progress-current">Generating: {onboardingProgress.current_page}</div>
					{/if}
					{#if onboardingProgress.total > 0}
						<div class="progress-bar-track">
							<div class="progress-bar-fill" style="width: {(onboardingProgress.generated / onboardingProgress.total) * 100}%"></div>
						</div>
					{/if}
					{#if onboardingProgress.errors.length > 0}
						<div class="progress-errors">
							{#each onboardingProgress.errors as err}
								<div class="progress-error-item">{err}</div>
							{/each}
						</div>
					{/if}
				</div>
			{/if}

			{#if onboardingResult}
				<div class="onboarding-result">
					<h4>Onboarding Complete</h4>
					<div class="result-stats">
						<span>Pages generated: {onboardingResult.pages_generated}</span>
						{#if onboardingResult.pages_skipped > 0}
							<span>Skipped: {onboardingResult.pages_skipped}</span>
						{/if}
						{#if onboardingResult.pages_failed > 0}
							<span class="result-failed">Failed: {onboardingResult.pages_failed}</span>
						{/if}
						<span>Links: {onboardingResult.links_created}</span>
						<span>Tokens: {onboardingResult.tokens_used}</span>
					</div>
				</div>
			{/if}
		</div>
	{/if}

	{#if error}
		<div class="error-banner">
			<span>⚠ {error}</span>
			<button onclick={() => (error = null)}>✕</button>
		</div>
	{/if}

	<nav class="view-tabs" aria-label="Wiki views">
		<button class:active={activeView === 'browse'} onclick={() => switchView('browse')}>Browse</button>
		<button class:active={activeView === 'search'} onclick={() => switchView('search')}>Search</button>
		<button class:active={activeView === 'query'} onclick={() => switchView('query')}>Ask Wiki</button>
		<button class:active={activeView === 'graph'} onclick={() => switchView('graph')}>Knowledge Graph</button>
	</nav>

	<!-- BROWSE VIEW -->
	{#if activeView === 'browse'}
		<section class="browse-section">
			<div class="filters">
				<select bind:value={typeFilter} onchange={loadPages}>
					<option value="">All Types</option>
					<option value="summary">Summary</option>
					<option value="entity">Entity</option>
					<option value="concept">Concept</option>
					<option value="answer">Answer</option>
					<option value="index">Index</option>
				</select>
				<select bind:value={statusFilter} onchange={loadPages}>
					<option value="">All Statuses</option>
					<option value="published">Published</option>
					<option value="draft">Draft</option>
					<option value="stale">Stale</option>
					<option value="archived">Archived</option>
				</select>
				<span class="count-label">{totalCount} pages</span>
			</div>

			{#if loading}
				<div class="loading">Loading pages...</div>
			{:else if pages.length === 0}
				<div class="empty-state">No wiki pages found.</div>
			{:else}
				<div class="pages-list">
					{#each pages as page}
						<div
							class="page-card"
							class:expanded={selectedPage?.id === page.id}
							role="button"
							tabindex="0"
							onclick={() => (selectedPage = selectedPage?.id === page.id ? null : page)}
							onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); selectedPage = selectedPage?.id === page.id ? null : page } }}
						>
							<div class="page-card-header">
								<div class="page-title-row">
									<span class="page-title">{page.title}</span>
									<div class="badges">
										<span class="badge {typeBadgeClass(page.page_type)}">{page.page_type}</span>
										<span class="badge {statusBadgeClass(page.status)}">{page.status}</span>
									</div>
								</div>
								<div class="page-meta">
									<span class="meta-item">v{page.version}</span>
									<span class="meta-item">Updated {formatDate(page.updated_at)}</span>
									{#if page.tags.length > 0}
										<span class="meta-item tags">
											{#each page.tags.slice(0, 3) as tag}
												<span class="tag">{tag}</span>
											{/each}
											{#if page.tags.length > 3}
												<span class="tag tag-more">+{page.tags.length - 3}</span>
											{/if}
										</span>
									{/if}
								</div>
								<div
									class="staleness-bar"
									title="Staleness: {(page.staleness_score * 100).toFixed(0)}%"
									style="background: {stalenessColor(page.staleness_score)}; width: {Math.max(page.staleness_score * 100, 2)}%"
								></div>
							</div>

							{#if selectedPage?.id === page.id}
								<div class="page-detail">
									<div class="page-content">{page.content}</div>
									{#if page.generated_by}
										<div class="page-footer">
											<span class="generated-by">Generated by: {page.generated_by}</span>
										</div>
									{/if}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</section>
	{/if}

	<!-- SEARCH VIEW -->
	{#if activeView === 'search'}
		<section class="search-section">
			<form class="search-form" onsubmit={(e) => { e.preventDefault(); doSearch() }}>
				<input
					type="search"
					bind:value={searchQuery}
					placeholder="Search wiki pages..."
					aria-label="Search wiki"
				/>
				<button type="submit" disabled={searchLoading}>
					{searchLoading ? 'Searching...' : 'Search'}
				</button>
			</form>

			{#if searchLoading}
				<div class="loading">Searching...</div>
			{:else if searchResults.length > 0}
				<div class="search-results">
					<p class="results-count">{searchResults.length} results for "{searchQuery}"</p>
					{#each searchResults as page}
						<article class="result-card">
							<div class="result-header">
								<span class="result-title">{page.title}</span>
								<span class="badge {typeBadgeClass(page.page_type)}">{page.page_type}</span>
								<span class="badge {statusBadgeClass(page.status)}">{page.status}</span>
							</div>
							<p class="result-excerpt">{page.content.slice(0, 200)}{page.content.length > 200 ? '...' : ''}</p>
							<div class="result-meta">Updated {formatDate(page.updated_at)}</div>
						</article>
					{/each}
				</div>
			{:else if searchQuery}
				<div class="empty-state">No results for "{searchQuery}"</div>
			{/if}
		</section>
	{/if}

	<!-- QUERY VIEW -->
	{#if activeView === 'query'}
		<section class="query-section">
			<p class="query-hint">Ask a natural language question about the project. The wiki will synthesize an answer from relevant pages.</p>

			<form class="query-form" onsubmit={(e) => { e.preventDefault(); doQuery() }}>
				<textarea
					bind:value={queryInput}
					placeholder="e.g. How does the orchestration state machine work?"
					rows="3"
					aria-label="Wiki query"
				></textarea>
				<button type="submit" disabled={queryLoading || !queryInput.trim()}>
					{queryLoading ? 'Thinking...' : 'Ask Wiki'}
				</button>
			</form>

			{#if queryLoading}
				<div class="query-spinner">
					<div class="spinner" aria-label="Loading"></div>
					<span>Synthesizing answer...</span>
				</div>
			{:else if queryError}
				<div class="error-banner">
					<span>⚠ {queryError}</span>
					<button onclick={() => (queryError = null)}>✕</button>
				</div>
			{:else if queryResult}
				<div class="query-result">
					<div class="answer-block">
						<h3>Answer</h3>
						<p class="answer-text">{queryResult.answer}</p>
						{#if queryResult.tokens_used}
							<span class="tokens-used">{queryResult.tokens_used} tokens used</span>
						{/if}
					</div>

					{#if queryResult.citations.length > 0}
						<div class="citations-block">
							<h4>Citations</h4>
							<ul class="citations-list">
								{#each queryResult.citations as citation}
									<li class="citation-item">
										<span class="citation-source">{citation.source_type}: {citation.source_id}</span>
										<span class="citation-relevance">relevance: {(citation.relevance * 100).toFixed(0)}%</span>
										<p class="citation-excerpt">{citation.excerpt}</p>
									</li>
								{/each}
							</ul>
						</div>
					{/if}
				</div>
			{/if}
		</section>
	{/if}

	<!-- GRAPH VIEW -->
	{#if activeView === 'graph'}
		<section class="graph-section">
			{#if graphLoading}
				<div class="loading">Loading knowledge graph...</div>
			{:else if !graphData}
				<div class="empty-state">No graph data available.</div>
			{:else if graphData.nodes.length === 0}
				<div class="empty-state">No nodes in the knowledge graph yet.</div>
			{:else}
				<div class="graph-meta">
					<span>{graphData.nodes.length} nodes</span>
					<span>·</span>
					<span>{graphData.edges.length} edges</span>
				</div>
				<div class="graph-container">
					<svg
						width={graphWidth}
						height={graphHeight}
						role="img"
						aria-label="Knowledge graph"
					>
						<defs>
							<marker id="arrowhead" markerWidth="6" markerHeight="4" refX="6" refY="2" orient="auto">
								<polygon points="0 0, 6 2, 0 4" fill="#444d56" />
							</marker>
						</defs>

						<!-- Edges -->
						{#each graphData.edges as edge}
							{@const fromPos = graphPositions.get(edge.from)}
							{@const toPos = graphPositions.get(edge.to)}
							{#if fromPos && toPos}
								<line
									x1={fromPos.x}
									y1={fromPos.y}
									x2={toPos.x}
									y2={toPos.y}
									stroke="#30363d"
									stroke-width={Math.max(1, edge.strength * 3)}
									stroke-opacity="0.7"
									marker-end="url(#arrowhead)"
								/>
							{/if}
						{/each}

						<!-- Nodes -->
						{#each graphData.nodes as node}
							{@const pos = graphPositions.get(node.id)}
							{#if pos}
								<g class="graph-node" transform="translate({pos.x},{pos.y})">
									<circle
										r="20"
										fill="#21262d"
										stroke={stalenessColor(node.staleness_score)}
										stroke-width="2"
									/>
									<text
										text-anchor="middle"
										dy="35"
										font-size="10"
										fill="#8b949e"
										class="node-label"
									>{node.title.slice(0, 18)}{node.title.length > 18 ? '…' : ''}</text>
									<title>{node.title} ({node.page_type}, {node.status})</title>
								</g>
							{/if}
						{/each}
					</svg>
				</div>
				<p class="graph-legend">Node border color indicates staleness: <span style="color:#58a6ff">fresh</span> · <span style="color:#3fb950">minor</span> · <span style="color:#d29922">moderate</span> · <span style="color:#f85149">stale</span></p>
			{/if}
		</section>
	{/if}
</div>

<style>
	.wiki {
		padding: 2rem;
		max-width: 1200px;
		margin: 0 auto;
	}

	header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		margin-bottom: 1.5rem;
		padding-bottom: 1rem;
		border-bottom: 1px solid #30363d;
		gap: 1rem;
		flex-wrap: wrap;
	}

	.header-left h1 {
		font-size: 1.75rem;
		margin: 0;
		color: #58a6ff;
	}

	.subtitle {
		color: #8b949e;
		margin-top: 0.25rem;
		font-size: 0.875rem;
	}

	.vault-status {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		flex-wrap: wrap;
	}

	.vault-info {
		font-size: 0.8rem;
		color: #8b949e;
	}

	.sync-btn {
		padding: 0.4rem 0.9rem;
		border-radius: 6px;
		border: 1px solid #30363d;
		background: #21262d;
		color: #c9d1d9;
		cursor: pointer;
		font-size: 0.8rem;
		transition: background 0.15s, color 0.15s;
	}
	.sync-btn:hover:not(:disabled) {
		background: #30363d;
		color: #ffffff;
	}
	.sync-btn:disabled {
		opacity: 0.6;
		cursor: default;
	}

	.sync-msg {
		font-size: 0.78rem;
		color: #3fb950;
	}

	.error-banner {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.75rem 1rem;
		margin-bottom: 1rem;
		background: #2d1117;
		border: 1px solid #f85149;
		border-radius: 6px;
		color: #f85149;
		font-size: 0.875rem;
	}
	.error-banner button {
		background: none;
		border: none;
		color: #f85149;
		cursor: pointer;
		font-size: 1rem;
		padding: 0 4px;
	}

	.view-tabs {
		display: flex;
		gap: 4px;
		margin-bottom: 1.5rem;
		border-bottom: 1px solid #30363d;
		padding-bottom: 0;
	}

	.view-tabs button {
		padding: 0.5rem 1rem;
		background: transparent;
		border: none;
		border-bottom: 2px solid transparent;
		color: #8b949e;
		cursor: pointer;
		font-size: 0.875rem;
		transition: color 0.15s, border-color 0.15s;
		margin-bottom: -1px;
	}
	.view-tabs button:hover {
		color: #c9d1d9;
	}
	.view-tabs button.active {
		color: #58a6ff;
		border-bottom-color: #58a6ff;
	}

	/* Filters */
	.filters {
		display: flex;
		gap: 0.75rem;
		align-items: center;
		margin-bottom: 1rem;
		flex-wrap: wrap;
	}

	.filters select {
		padding: 0.35rem 0.6rem;
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		color: #c9d1d9;
		font-size: 0.8rem;
		cursor: pointer;
	}

	.count-label {
		font-size: 0.8rem;
		color: #8b949e;
		margin-left: auto;
	}

	/* Loading / empty */
	.loading {
		text-align: center;
		padding: 3rem;
		color: #8b949e;
	}

	.empty-state {
		text-align: center;
		padding: 3rem;
		color: #6e7681;
		font-size: 0.875rem;
	}

	/* Pages list */
	.pages-list {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}

	.page-card {
		background: #161b22;
		border: 1px solid #30363d;
		border-radius: 8px;
		padding: 0.875rem 1rem;
		cursor: pointer;
		transition: border-color 0.15s, background 0.15s;
	}
	.page-card:hover {
		border-color: #58a6ff;
		background: #1c2128;
	}
	.page-card.expanded {
		border-color: #58a6ff;
	}

	.page-card-header {
		display: flex;
		flex-direction: column;
		gap: 0.375rem;
	}

	.page-title-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.page-title {
		font-weight: 500;
		color: #c9d1d9;
		flex: 1;
		min-width: 0;
	}

	.page-meta {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		font-size: 0.75rem;
		color: #8b949e;
		flex-wrap: wrap;
	}

	.staleness-bar {
		height: 2px;
		border-radius: 1px;
		margin-top: 0.25rem;
		transition: width 0.3s;
		opacity: 0.7;
	}

	.page-detail {
		margin-top: 0.875rem;
		padding-top: 0.875rem;
		border-top: 1px solid #30363d;
	}

	.page-content {
		font-size: 0.85rem;
		color: #8b949e;
		white-space: pre-wrap;
		line-height: 1.6;
		max-height: 400px;
		overflow-y: auto;
	}

	.page-footer {
		margin-top: 0.75rem;
		font-size: 0.75rem;
		color: #6e7681;
	}

	/* Badges */
	.badges {
		display: flex;
		gap: 0.375rem;
		flex-shrink: 0;
	}

	.badge {
		font-size: 0.7rem;
		padding: 2px 7px;
		border-radius: 10px;
		font-weight: 500;
		white-space: nowrap;
	}

	.badge-published { background: #1a3a2a; color: #3fb950; border: 1px solid #2ea043; }
	.badge-draft     { background: #21262d; color: #8b949e; border: 1px solid #30363d; }
	.badge-stale     { background: #2d2208; color: #d29922; border: 1px solid #9e6a03; }
	.badge-archived  { background: #161b22; color: #6e7681; border: 1px solid #30363d; }
	.badge-default   { background: #21262d; color: #8b949e; border: 1px solid #30363d; }

	.type-summary { background: #1c2a3a; color: #58a6ff; border: 1px solid #1f6feb; }
	.type-entity  { background: #2a1c3a; color: #bc8cff; border: 1px solid #8957e5; }
	.type-concept { background: #1a2d2a; color: #56d364; border: 1px solid #2ea043; }
	.type-answer  { background: #2d2208; color: #ffa657; border: 1px solid #9e6a03; }
	.type-index   { background: #21262d; color: #8b949e; border: 1px solid #30363d; }
	.type-default { background: #21262d; color: #8b949e; border: 1px solid #30363d; }

	.tags { display: flex; gap: 0.25rem; }
	.tag {
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 4px;
		padding: 1px 5px;
		font-size: 0.7rem;
		color: #8b949e;
	}
	.tag-more { color: #6e7681; }

	/* Search */
	.search-form {
		display: flex;
		gap: 0.5rem;
		margin-bottom: 1.25rem;
	}

	.search-form input {
		flex: 1;
		padding: 0.5rem 0.75rem;
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		color: #c9d1d9;
		font-size: 0.875rem;
	}
	.search-form input:focus {
		outline: none;
		border-color: #58a6ff;
	}

	.search-form button {
		padding: 0.5rem 1.25rem;
		background: #1f6feb;
		border: none;
		border-radius: 6px;
		color: #ffffff;
		cursor: pointer;
		font-size: 0.875rem;
		transition: background 0.15s;
	}
	.search-form button:hover:not(:disabled) {
		background: #388bfd;
	}
	.search-form button:disabled {
		opacity: 0.6;
		cursor: default;
	}

	.results-count {
		font-size: 0.8rem;
		color: #8b949e;
		margin-bottom: 0.75rem;
	}

	.search-results {
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
	}

	.result-card {
		background: #161b22;
		border: 1px solid #30363d;
		border-radius: 8px;
		padding: 0.875rem 1rem;
	}

	.result-header {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin-bottom: 0.5rem;
		flex-wrap: wrap;
	}

	.result-title {
		font-weight: 500;
		color: #c9d1d9;
		flex: 1;
	}

	.result-excerpt {
		font-size: 0.8rem;
		color: #8b949e;
		line-height: 1.5;
		margin: 0 0 0.375rem;
	}

	.result-meta {
		font-size: 0.75rem;
		color: #6e7681;
	}

	/* Query */
	.query-hint {
		font-size: 0.85rem;
		color: #8b949e;
		margin-bottom: 1rem;
	}

	.query-form {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		margin-bottom: 1.5rem;
	}

	.query-form textarea {
		padding: 0.625rem 0.875rem;
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		color: #c9d1d9;
		font-size: 0.875rem;
		resize: vertical;
		font-family: inherit;
	}
	.query-form textarea:focus {
		outline: none;
		border-color: #58a6ff;
	}

	.query-form button {
		align-self: flex-start;
		padding: 0.5rem 1.5rem;
		background: #1f6feb;
		border: none;
		border-radius: 6px;
		color: #ffffff;
		cursor: pointer;
		font-size: 0.875rem;
		transition: background 0.15s;
	}
	.query-form button:hover:not(:disabled) {
		background: #388bfd;
	}
	.query-form button:disabled {
		opacity: 0.6;
		cursor: default;
	}

	.query-spinner {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 2rem;
		color: #8b949e;
		font-size: 0.875rem;
	}

	.spinner {
		width: 20px;
		height: 20px;
		border: 2px solid #30363d;
		border-top-color: #58a6ff;
		border-radius: 50%;
		animation: spin 0.8s linear infinite;
		flex-shrink: 0;
	}

	@keyframes spin {
		to { transform: rotate(360deg); }
	}

	.query-result {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
	}

	.answer-block {
		background: #161b22;
		border: 1px solid #30363d;
		border-left: 3px solid #58a6ff;
		border-radius: 8px;
		padding: 1.25rem;
	}

	.answer-block h3 {
		font-size: 0.875rem;
		font-weight: 600;
		color: #58a6ff;
		margin: 0 0 0.75rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.answer-text {
		font-size: 0.9rem;
		color: #c9d1d9;
		line-height: 1.7;
		white-space: pre-wrap;
		margin: 0;
	}

	.tokens-used {
		display: inline-block;
		margin-top: 0.75rem;
		font-size: 0.75rem;
		color: #6e7681;
	}

	.citations-block h4 {
		font-size: 0.8rem;
		font-weight: 600;
		color: #8b949e;
		margin: 0 0 0.625rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.citations-list {
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
	}

	.citation-item {
		background: #161b22;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 0.75rem 1rem;
	}

	.citation-source {
		font-size: 0.75rem;
		font-weight: 600;
		color: #58a6ff;
	}

	.citation-relevance {
		font-size: 0.7rem;
		color: #8b949e;
		margin-left: 0.5rem;
	}

	.citation-excerpt {
		margin: 0.375rem 0 0;
		font-size: 0.8rem;
		color: #8b949e;
		line-height: 1.5;
	}

	/* Graph */
	.graph-meta {
		font-size: 0.8rem;
		color: #8b949e;
		margin-bottom: 0.75rem;
		display: flex;
		gap: 0.5rem;
	}

	.graph-container {
		background: #0d1117;
		border: 1px solid #30363d;
		border-radius: 8px;
		overflow: auto;
		max-height: 600px;
	}

	.graph-container svg {
		display: block;
	}

	.graph-node {
		cursor: default;
	}

	.node-label {
		pointer-events: none;
	}

	.graph-legend {
		margin-top: 0.75rem;
		font-size: 0.75rem;
		color: #6e7681;
	}

	.onboard-btn {
		padding: 0.4rem 0.9rem;
		border-radius: 6px;
		border: 1px solid #1f6feb;
		background: #1f6feb;
		color: #ffffff;
		cursor: pointer;
		font-size: 0.8rem;
		transition: background 0.15s;
	}
	.onboard-btn:hover:not(:disabled) {
		background: #388bfd;
	}
	.onboard-btn:disabled {
		opacity: 0.6;
		cursor: default;
	}

	.onboarding-panel {
		background: #161b22;
		border: 1px solid #30363d;
		border-radius: 8px;
		padding: 1.25rem;
		margin-bottom: 1.5rem;
	}

	.onboarding-header h3 {
		margin: 0 0 0.25rem;
		color: #c9d1d9;
		font-size: 1rem;
	}

	.onboarding-desc {
		color: #8b949e;
		font-size: 0.85rem;
		margin: 0 0 1rem;
	}

	.onboarding-controls {
		display: flex;
		gap: 1rem;
		align-items: center;
		margin-bottom: 1rem;
		flex-wrap: wrap;
	}

	.depth-label {
		font-size: 0.85rem;
		color: #8b949e;
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.depth-label select {
		padding: 0.35rem 0.6rem;
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		color: #c9d1d9;
		font-size: 0.8rem;
	}

	.start-btn {
		padding: 0.5rem 1.25rem;
		background: #1f6feb;
		border: none;
		border-radius: 6px;
		color: #ffffff;
		cursor: pointer;
		font-size: 0.85rem;
	}
	.start-btn:hover:not(:disabled) {
		background: #388bfd;
	}
	.start-btn:disabled {
		opacity: 0.6;
		cursor: default;
	}

	.onboarding-error {
		color: #f85149;
		font-size: 0.85rem;
		margin-bottom: 0.75rem;
	}

	.onboarding-progress {
		margin-top: 0.75rem;
	}

	.progress-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 0.375rem;
	}

	.progress-status {
		font-size: 0.8rem;
		color: #58a6ff;
		text-transform: capitalize;
	}

	.progress-count {
		font-size: 0.8rem;
		color: #8b949e;
	}

	.progress-current {
		font-size: 0.8rem;
		color: #8b949e;
		margin-bottom: 0.5rem;
	}

	.progress-bar-track {
		height: 4px;
		background: #21262d;
		border-radius: 2px;
		overflow: hidden;
	}

	.progress-bar-fill {
		height: 100%;
		background: #1f6feb;
		border-radius: 2px;
		transition: width 0.3s;
	}

	.progress-errors {
		margin-top: 0.5rem;
	}

	.progress-error-item {
		font-size: 0.78rem;
		color: #f85149;
		padding: 0.25rem 0;
	}

	.onboarding-result {
		margin-top: 0.75rem;
		padding: 0.75rem;
		background: #1a3a2a;
		border: 1px solid #2ea043;
		border-radius: 6px;
	}

	.onboarding-result h4 {
		margin: 0 0 0.5rem;
		color: #3fb950;
		font-size: 0.875rem;
	}

	.result-stats {
		display: flex;
		gap: 1rem;
		flex-wrap: wrap;
		font-size: 0.8rem;
		color: #8b949e;
	}

	.result-failed {
		color: #f85149;
	}
</style>
