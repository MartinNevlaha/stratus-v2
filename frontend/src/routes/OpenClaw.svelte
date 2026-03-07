<script lang="ts">
	import { onMount } from 'svelte'
	import { appState } from '$lib/store'

	let loading = $state(true)
	let error = $state<string | null>(null)
	let dashboard = $state<any>(null)
	let patterns = $state<any[]>([])
	let analyses = $state<any[]>([])
	let proposals = $state<any[]>([])
	let selectedProposal = $state<any>(null)
	let actionLoading = $state(false)
	let showReasonInput = $state(false)
	let reason = $state('')

	let agentScorecards = $state<any[]>([])
	let workflowScorecards = $state<any[]>([])
	let scorecardHighlights = $state<any>(null)
	let scorecardWindow = $state('7d')

	let routingRecommendations = $state<any[]>([])
	let selectedRoutingRec = $state<any>(null)

	// Filters
	let proposalStatusFilter = $state('')
	let proposalTypeFilter = $state('')
	let proposalRiskFilter = $state('')
	let patternSeverityFilter = $state('')

	onMount(async () => {
		await Promise.all([
			loadDashboard(),
			loadPatterns(),
			loadAnalyses(),
			loadProposals(),
			loadRoutingRecommendations()
		])
	})

	async function loadDashboard() {
		try {
			const res = await fetch('/api/openclaw/dashboard')
			if (!res.ok) throw new Error('Failed to load dashboard')
			dashboard = await res.json()
		} catch (e) {
			console.error('Failed to load dashboard:', e)
		}
	}

	async function loadPatterns() {
		try {
			const params = new URLSearchParams()
			if (patternSeverityFilter) params.set('severity', patternSeverityFilter)
			params.set('limit', '20')
			
			const res = await fetch(`/api/openclaw/patterns?${params}`)
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

	async function loadProposals() {
		try {
			const params = new URLSearchParams()
			if (proposalStatusFilter) params.set('status', proposalStatusFilter)
			if (proposalTypeFilter) params.set('type', proposalTypeFilter)
			if (proposalRiskFilter) params.set('risk', proposalRiskFilter)
			params.set('limit', '50')
			
			const res = await fetch(`/api/openclaw/proposals?${params}`)
			if (!res.ok) throw new Error('Failed to load proposals')
			const data = await res.json()
			proposals = data.proposals || []
		} catch (e) {
			console.error('Failed to load proposals:', e)
		}
	}

	async function loadScorecards() {
		await Promise.all([
			loadAgentScorecards(),
			loadWorkflowScorecards()
		])
	}

	async function loadAgentScorecards() {
		try {
			const params = new URLSearchParams()
			params.set('window', scorecardWindow)
			params.set('limit', '50')
			
			const res = await fetch(`/api/openclaw/scorecards/agents?${params}`)
			if (!res.ok) throw new Error('Failed to load agent scorecards')
			const data = await res.json()
			agentScorecards = data.scorecards || []
			scorecardHighlights = data.highlights || null
		} catch (e) {
			console.error('Failed to load agent scorecards:', e)
		}
	}

	async function loadWorkflowScorecards() {
		try {
			const params = new URLSearchParams()
			params.set('window', scorecardWindow)
			params.set('limit', '50')
			
			const res = await fetch(`/api/openclaw/scorecards/workflows?${params}`)
			if (!res.ok) throw new Error('Failed to load workflow scorecards')
			const data = await res.json()
			workflowScorecards = data.scorecards || []
		} catch (e) {
			console.error('Failed to load workflow scorecards:', e)
		}
	}

	async function triggerScorecardComputation() {
		try {
			const res = await fetch('/api/openclaw/scorecards/compute', { method: 'POST' })
			if (!res.ok) throw new Error('Failed to trigger scorecard computation')
			await new Promise(resolve => setTimeout(resolve, 2000))
			await loadScorecards()
		} catch (e) {
			error = e instanceof Error ? e.message : 'Unknown error'
		}
	}

	async function loadRoutingRecommendations() {
		try {
			const res = await fetch('/api/openclaw/routing/recommendations?limit=50')
			if (!res.ok) throw new Error('Failed to load routing recommendations')
			const data = await res.json()
			routingRecommendations = data.recommendations || []
		} catch (e) {
			console.error('Failed to load routing recommendations:', e)
		}
	}

	async function triggerRoutingAnalysis() {
		try {
			const res = await fetch('/api/openclaw/routing/analyze', { method: 'POST' })
			if (!res.ok) throw new Error('Failed to trigger routing analysis')
			await new Promise(resolve => setTimeout(resolve, 2000))
			await loadRoutingRecommendations()
		} catch (e) {
			error = e instanceof Error ? e.message : 'Unknown error'
		}
	}

	function formatRecType(type: string) {
		switch (type) {
			case 'best_agent': return 'Best Agent'
			case 'deprioritize': return 'Deprioritize'
			case 'fallback_needed': return 'Fallback Needed'
			case 'instability': return 'Instability'
			default: return type
		}
	}

	async function loadProposalDetail(id: string) {
		try {
			const res = await fetch(`/api/openclaw/proposals/${id}`)
			if (!res.ok) throw new Error('Failed to load proposal')
			selectedProposal = await res.json()
		} catch (e) {
			error = e instanceof Error ? e.message : 'Unknown error'
		}
	}

	async function updateProposalStatus(newStatus: string, requireReason: boolean = false) {
		if (!selectedProposal) return
		
		if (requireReason && !reason.trim()) {
			showReasonInput = true
			return
		}

		try {
			actionLoading = true
			const res = await fetch(`/api/openclaw/proposals/${selectedProposal.id}/status`, {
				method: 'PATCH',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					status: newStatus,
					reason: reason.trim() || undefined
				})
			})

			if (!res.ok) {
				const data = await res.json()
				throw new Error(data.error || 'Failed to update status')
			}

			selectedProposal = await res.json()
			showReasonInput = false
			reason = ''
			await loadProposals()
			await loadDashboard()
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to update status'
		} finally {
			actionLoading = false
		}
	}

	async function triggerAnalysis() {
		try {
			const res = await fetch('/api/openclaw/trigger', { method: 'POST' })
			if (!res.ok) throw new Error('Failed to trigger analysis')
			await new Promise(resolve => setTimeout(resolve, 2000))
			await Promise.all([loadDashboard(), loadPatterns(), loadAnalyses(), loadProposals()])
		} catch (e) {
			error = e instanceof Error ? e.message : 'Unknown error'
		}
	}

	function formatDate(dateStr: string) {
		return new Date(dateStr).toLocaleString()
	}

	function getConfidenceColor(confidence: number) {
		if (confidence >= 0.8) return '#3fb950'
		if (confidence >= 0.6) return '#d29922'
		return '#f85149'
	}

	function getStatusBadgeClass(status: string) {
		return `badge-${status}`
	}

	function getRiskBadgeClass(risk: string) {
		return `risk-${risk}`
	}

	function getSeverityBadgeClass(severity: string) {
		return `severity-${severity}`
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

	function getAvailableActions(status: string) {
		const actions: Array<{ label: string; action: string; requireReason: boolean; class: string }> = []
		
		switch (status) {
			case 'detected':
				actions.push({ label: 'Mark as Drafted', action: 'drafted', requireReason: false, class: 'drafted' })
				break
			case 'drafted':
				actions.push(
					{ label: 'Approve', action: 'approved', requireReason: false, class: 'approved' },
					{ label: 'Reject', action: 'rejected', requireReason: true, class: 'rejected' }
				)
				break
			case 'approved':
			case 'rejected':
				actions.push({ label: 'Archive', action: 'archived', requireReason: false, class: 'archived' })
				break
		}
		
		return actions
	}

	function getTrendIcon(trend: string) {
		switch (trend) {
			case 'improving': return '↑'
			case 'degrading': return '↓'
			default: return '→'
		}
	}

	function getTrendColor(trend: string) {
		switch (trend) {
			case 'improving': return '#3fb950'
			case 'degrading': return '#f85149'
			default: return '#8b949e'
		}
	}

	function formatPercent(value: number) {
		return (value * 100).toFixed(1) + '%'
	}

	function formatDuration(ms: number) {
		if (!ms || ms === 0) return '0ms'
		if (ms < 1000) return ms + 'ms'
		if (ms < 60000) return (ms / 1000).toFixed(1) + 's'
		return (ms / 60000).toFixed(1) + 'm'
	}
</script>

<div class="openclaw">
	<header>
		<div class="header-left">
			<h1>OpenClaw - AI Coach</h1>
			<p class="subtitle">Autonomous pattern detection & improvement suggestions</p>
		</div>
		<div class="controls">
			<button class="refresh-btn" onclick={() => Promise.all([loadDashboard(), loadPatterns(), loadAnalyses(), loadProposals(), loadRoutingRecommendations()])} disabled={loading}>
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
			<button onclick={() => error = null}>✕</button>
		</div>
	{/if}

	{#if loading && !dashboard}
		<div class="loading">Loading OpenClaw status...</div>
	{:else}
		<div class="dashboard">
			<!-- Dashboard Summary -->
			{#if dashboard}
				<section class="summary-section">
					<h2>Dashboard Summary (Last 24h)</h2>
					<div class="summary-grid">
						<div class="summary-card">
							<div class="summary-label">Recent Proposals</div>
							<div class="summary-value">{dashboard.recent_proposals || 0}</div>
						</div>
						<div class="summary-card">
							<div class="summary-label">Recent Patterns</div>
							<div class="summary-value">{dashboard.recent_patterns || 0}</div>
						</div>
						<div class="summary-card">
							<div class="summary-label">Approved</div>
							<div class="summary-value">{dashboard.proposals_by_status?.approved || 0}</div>
						</div>
						<div class="summary-card">
							<div class="summary-label">Rejected</div>
							<div class="summary-value">{dashboard.proposals_by_status?.rejected || 0}</div>
						</div>
						<div class="summary-card">
							<div class="summary-label">Critical/High Patterns</div>
							<div class="summary-value">{(dashboard.patterns_by_severity?.critical || 0) + (dashboard.patterns_by_severity?.high || 0)}</div>
						</div>
					</div>
				</section>
			{/if}

			<!-- Agent Scorecards Section -->
			<section class="scorecards-section">
				<div class="section-header">
					<h2>Agent Scorecards</h2>
					<div class="scorecard-controls">
						<select bind:value={scorecardWindow} onchange={loadScorecards}>
							<option value="7d">7 Days</option>
							<option value="30d">30 Days</option>
						</select>
						<button class="compute-btn" onclick={triggerScorecardComputation} disabled={loading}>
							Compute Scorecards
						</button>
					</div>
				</div>

				{#if scorecardHighlights}
					<div class="highlights-grid">
						{#if scorecardHighlights.best_agent}
							<div class="highlight-card positive">
								<div class="highlight-label">Best Agent</div>
								<div class="highlight-value">{scorecardHighlights.best_agent.agent_name}</div>
								<div class="highlight-detail">{formatPercent(scorecardHighlights.best_agent.success_rate)} success</div>
							</div>
						{/if}
						{#if scorecardHighlights.most_degraded_agent}
							<div class="highlight-card negative">
								<div class="highlight-label">Most Degraded</div>
								<div class="highlight-value">{scorecardHighlights.most_degraded_agent.agent_name}</div>
								<div class="highlight-detail">{formatPercent(scorecardHighlights.most_degraded_agent.success_rate)} success</div>
							</div>
						{/if}
						{#if scorecardHighlights.slowest_workflow}
							<div class="highlight-card warning">
								<div class="highlight-label">Slowest Workflow</div>
								<div class="highlight-value">{scorecardHighlights.slowest_workflow.workflow_type}</div>
								<div class="highlight-detail">{formatDuration(scorecardHighlights.slowest_workflow.avg_duration_ms)} avg</div>
							</div>
						{/if}
						{#if scorecardHighlights.highest_rework_workflow}
							<div class="highlight-card negative">
								<div class="highlight-label">Highest Rework</div>
								<div class="highlight-value">{scorecardHighlights.highest_rework_workflow.workflow_type}</div>
								<div class="highlight-detail">{formatPercent(scorecardHighlights.highest_rework_workflow.rework_rate)} rework</div>
							</div>
						{/if}
					</div>
				{/if}

				{#if agentScorecards.length === 0}
					<div class="empty-state">No agent scorecards available. Click "Compute Scorecards" to generate.</div>
				{:else}
					<table class="scorecards-table">
						<thead>
							<tr>
								<th>Agent</th>
								<th>Runs</th>
								<th>Success</th>
								<th>Failure</th>
								<th>Review Pass</th>
								<th>Rework</th>
								<th>Cycle Time</th>
								<th>Confidence</th>
								<th>Trend</th>
							</tr>
						</thead>
						<tbody>
							{#each agentScorecards as card}
								<tr>
									<td class="agent-name">{card.agent_name}</td>
									<td>{card.total_runs}</td>
									<td style="color: {getConfidenceColor(card.success_rate)}">{formatPercent(card.success_rate)}</td>
									<td style="color: {card.failure_rate > 0.3 ? '#f85149' : '#c9d1d9'}">{formatPercent(card.failure_rate)}</td>
									<td style="color: {getConfidenceColor(card.review_pass_rate)}">{formatPercent(card.review_pass_rate)}</td>
									<td style="color: {card.rework_rate > 0.3 ? '#f85149' : '#c9d1d9'}">{formatPercent(card.rework_rate)}</td>
									<td>{formatDuration(card.avg_cycle_time_ms)}</td>
									<td style="color: {getConfidenceColor(card.confidence_score)}">{formatPercent(card.confidence_score)}</td>
									<td>
										<span class="trend-badge trend-{card.trend}" style="color: {getTrendColor(card.trend)}">
											{getTrendIcon(card.trend)} {card.trend}
										</span>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				{/if}
			</section>

			<!-- Workflow Scorecards Section -->
			<section class="scorecards-section">
				<h2>Workflow Scorecards ({workflowScorecards.length})</h2>

				{#if workflowScorecards.length === 0}
					<div class="empty-state">No workflow scorecards available. Click "Compute Scorecards" above to generate.</div>
				{:else}
					<table class="scorecards-table">
						<thead>
							<tr>
								<th>Workflow Type</th>
								<th>Runs</th>
								<th>Completion</th>
								<th>Failure</th>
								<th>Rejection</th>
								<th>Rework</th>
								<th>Duration</th>
								<th>Confidence</th>
								<th>Trend</th>
							</tr>
						</thead>
						<tbody>
							{#each workflowScorecards as card}
								<tr>
									<td class="workflow-type">{card.workflow_type}</td>
									<td>{card.total_runs}</td>
									<td style="color: {getConfidenceColor(card.completion_rate)}">{formatPercent(card.completion_rate)}</td>
									<td style="color: {card.failure_rate > 0.3 ? '#f85149' : '#c9d1d9'}">{formatPercent(card.failure_rate)}</td>
									<td style="color: {card.review_rejection_rate > 0.3 ? '#f85149' : '#c9d1d9'}">{formatPercent(card.review_rejection_rate)}</td>
									<td style="color: {card.rework_rate > 0.3 ? '#f85149' : '#c9d1d9'}">{formatPercent(card.rework_rate)}</td>
									<td>{formatDuration(card.avg_duration_ms)}</td>
									<td style="color: {getConfidenceColor(card.confidence_score)}">{formatPercent(card.confidence_score)}</td>
									<td>
										<span class="trend-badge trend-{card.trend}" style="color: {getTrendColor(card.trend)}">
											{getTrendIcon(card.trend)} {card.trend}
										</span>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				{/if}
			</section>

			<!-- Routing Recommendations Section -->
			<section class="routing-section">
				<div class="section-header">
					<h2>Routing Recommendations</h2>
					<button class="analyze-btn" onclick={triggerRoutingAnalysis} disabled={loading}>
						Analyze Routing
					</button>
				</div>

				{#if routingRecommendations.length === 0}
					<div class="empty-state">No routing recommendations. Click "Analyze Routing" to generate recommendations.</div>
				{:else}
					<table class="routing-table">
						<thead>
							<tr>
								<th>Workflow</th>
								<th>Type</th>
								<th>Agent</th>
								<th>Confidence</th>
								<th>Risk</th>
								<th>Reason</th>
								<th>Created</th>
							</tr>
						</thead>
						<tbody>
							{#each routingRecommendations as rec}
								<tr onclick={() => selectedRoutingRec = rec} class:active={selectedRoutingRec?.id === rec.id}>
									<td class="workflow-name">{rec.workflow_type}</td>
									<td><span class="type-badge routing-{rec.recommendation_type}">{formatRecType(rec.recommendation_type)}</span></td>
									<td>{rec.recommended_agent || rec.current_agent || '-'}</td>
									<td style="color: {getConfidenceColor(rec.confidence)}">{formatPercent(rec.confidence)}</td>
									<td><span class="badge {getRiskBadgeClass(rec.risk_level)}">{rec.risk_level}</span></td>
									<td class="reason-cell">{rec.reason}</td>
									<td>{formatDate(rec.created_at)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				{/if}

				{#if selectedRoutingRec}
					<div class="routing-detail">
						<div class="detail-header">
							<h3>Recommendation Details</h3>
							<button class="close-btn" onclick={() => selectedRoutingRec = null}>✕</button>
						</div>
						<div class="detail-block">
							<h4>Evidence</h4>
							<pre class="json-block">{JSON.stringify(selectedRoutingRec.evidence, null, 2)}</pre>
						</div>
						<div class="detail-block">
							<h4>Metrics</h4>
							<div class="metrics-grid">
								<div class="metric-item">
									<span class="metric-label">Observations</span>
									<span class="metric-value">{selectedRoutingRec.observations}</span>
								</div>
								{#if selectedRoutingRec.evidence.agent_success_rate !== undefined}
									<div class="metric-item">
										<span class="metric-label">Agent Success Rate</span>
										<span class="metric-value">{formatPercent(selectedRoutingRec.evidence.agent_success_rate)}</span>
									</div>
								{/if}
								{#if selectedRoutingRec.evidence.workflow_failure_rate !== undefined}
									<div class="metric-item">
										<span class="metric-label">Workflow Failure Rate</span>
										<span class="metric-value">{formatPercent(selectedRoutingRec.evidence.workflow_failure_rate)}</span>
									</div>
								{/if}
								{#if selectedRoutingRec.evidence.rework_rate !== undefined}
									<div class="metric-item">
										<span class="metric-label">Rework Rate</span>
										<span class="metric-value">{formatPercent(selectedRoutingRec.evidence.rework_rate)}</span>
									</div>
								{/if}
							</div>
						</div>
					</div>
				{/if}
			</section>

			<!-- Proposals Section -->
			<section class="proposals-section">
				<h2>Recent Proposals ({proposals.length})</h2>
				
				<div class="filters">
					<select bind:value={proposalStatusFilter} onchange={loadProposals}>
						<option value="">All Statuses</option>
						<option value="detected">Detected</option>
						<option value="drafted">Drafted</option>
						<option value="approved">Approved</option>
						<option value="rejected">Rejected</option>
						<option value="archived">Archived</option>
					</select>
					<select bind:value={proposalTypeFilter} onchange={loadProposals}>
						<option value="">All Types</option>
						<option value="routing.change">Routing Change</option>
						<option value="workflow.investigate">Workflow Investigate</option>
						<option value="review_gate.add">Review Gate Add</option>
						<option value="agent.deprioritize">Agent Deprioritize</option>
						<option value="retry_policy.adjust">Retry Policy Adjust</option>
					</select>
					<select bind:value={proposalRiskFilter} onchange={loadProposals}>
						<option value="">All Risks</option>
						<option value="high">High</option>
						<option value="medium">Medium</option>
						<option value="low">Low</option>
					</select>
				</div>

				{#if proposals.length === 0}
					<div class="empty-state">No proposals found</div>
				{:else}
					<table class="proposals-table">
						<thead>
							<tr>
								<th>Title</th>
								<th>Type</th>
								<th>Status</th>
								<th>Risk</th>
								<th>Confidence</th>
								<th>Created</th>
							</tr>
						</thead>
						<tbody>
							{#each proposals as proposal}
								<tr onclick={() => loadProposalDetail(proposal.id)} class:active={selectedProposal?.id === proposal.id}>
									<td>{proposal.title}</td>
									<td><span class="type-badge">{proposal.type}</span></td>
									<td><span class="badge {getStatusBadgeClass(proposal.status)}">{proposal.status}</span></td>
									<td><span class="badge {getRiskBadgeClass(proposal.risk_level)}">{proposal.risk_level}</span></td>
									<td>
										<span style="color: {getConfidenceColor(proposal.confidence)}">
											{(proposal.confidence * 100).toFixed(0)}%
										</span>
									</td>
									<td>{formatDate(proposal.created_at)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				{/if}
			</section>

			<!-- Proposal Detail Panel -->
			{#if selectedProposal}
				<section class="detail-section">
					<div class="detail-header">
						<h2>Proposal Detail</h2>
						<button class="close-btn" onclick={() => selectedProposal = null}>✕</button>
					</div>

					<div class="detail-content">
						<div class="detail-title">
							<h3>{selectedProposal.title}</h3>
							<div class="badges">
								<span class="badge {getStatusBadgeClass(selectedProposal.status)}">{selectedProposal.status}</span>
								<span class="badge {getRiskBadgeClass(selectedProposal.risk_level)}">{selectedProposal.risk_level} risk</span>
								<span class="type-badge">{selectedProposal.type}</span>
							</div>
						</div>

						<div class="detail-meta">
							<div class="meta-item">
								<span class="label">Confidence</span>
								<div class="confidence-bar">
									<div class="confidence-fill" style="width: {selectedProposal.confidence * 100}%; background: {getConfidenceColor(selectedProposal.confidence)}"></div>
									<span class="confidence-value" style="color: {getConfidenceColor(selectedProposal.confidence)}">
										{(selectedProposal.confidence * 100).toFixed(0)}%
									</span>
								</div>
							</div>
							<div class="meta-item">
								<span class="label">Created</span>
								<span class="value">{formatDate(selectedProposal.created_at)}</span>
							</div>
							<div class="meta-item">
								<span class="label">Updated</span>
								<span class="value">{formatDate(selectedProposal.updated_at)}</span>
							</div>
						</div>

						<div class="detail-block">
							<h4>Description</h4>
							<p>{selectedProposal.description}</p>
						</div>

						{#if selectedProposal.decision_reason}
							<div class="detail-block">
								<h4>Decision Reason</h4>
								<p class="decision-reason">{selectedProposal.decision_reason}</p>
							</div>
						{/if}

						<div class="detail-block">
							<h4>Evidence</h4>
							<pre class="json-block">{JSON.stringify(selectedProposal.evidence, null, 2)}</pre>
						</div>

						<div class="detail-block">
							<h4>Recommendation</h4>
							<pre class="json-block">{JSON.stringify(selectedProposal.recommendation, null, 2)}</pre>
						</div>

						{#if getAvailableActions(selectedProposal.status).length > 0}
							<div class="detail-actions">
								<h4>Actions</h4>
								
								{#if showReasonInput}
									<div class="reason-input">
										<label for="reason">Please provide a reason:</label>
										<textarea id="reason" bind:value={reason} placeholder="Enter reason for this decision..." rows="3"></textarea>
									</div>
								{/if}

								<div class="action-buttons">
									{#each getAvailableActions(selectedProposal.status) as action}
										<button
											class="action-btn {action.class}"
											onclick={() => updateProposalStatus(action.action, action.requireReason)}
											disabled={actionLoading}
										>
											{action.label}
										</button>
									{/each}
								</div>
							</div>
						{/if}
					</div>
				</section>
			{/if}

			<!-- Patterns Section -->
			<section class="patterns-section">
				<h2>Recent Patterns ({patterns.length})</h2>
				
				<div class="filters">
					<select bind:value={patternSeverityFilter} onchange={loadPatterns}>
						<option value="">All Severities</option>
						<option value="critical">Critical</option>
						<option value="high">High</option>
						<option value="medium">Medium</option>
						<option value="low">Low</option>
					</select>
				</div>

				{#if patterns.length === 0}
					<div class="empty-state">No patterns detected yet</div>
				{:else}
					<div class="patterns-list">
						{#each patterns as pattern}
							<div class="pattern-card">
								<div class="pattern-header">
									<span class="pattern-icon">{getPatternTypeIcon(pattern.pattern_type)}</span>
									<span class="pattern-name">{pattern.pattern_name}</span>
									<span class="badge {getSeverityBadgeClass(pattern.severity)}">{pattern.severity}</span>
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

	.error-banner button {
		margin-left: auto;
		background: none;
		border: none;
		color: #f85149;
		cursor: pointer;
		font-size: 1.2rem;
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

	/* Summary Section */
	.summary-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 1rem;
	}

	.summary-card {
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 1.5rem;
		text-align: center;
	}

	.summary-label {
		font-size: 0.875rem;
		color: #8b949e;
		margin-bottom: 0.5rem;
	}

	.summary-value {
		font-size: 2rem;
		font-weight: 600;
		color: #c9d1d9;
	}

	/* Filters */
	.filters {
		display: flex;
		gap: 0.75rem;
		margin-bottom: 1.5rem;
		flex-wrap: wrap;
	}

	.filters select {
		padding: 0.5rem 1rem;
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		color: #c9d1d9;
		font-size: 0.9rem;
		cursor: pointer;
	}

	.filters select:hover {
		border-color: #58a6ff;
	}

	/* Proposals Table */
	.proposals-table {
		width: 100%;
		border-collapse: collapse;
	}

	.proposals-table th {
		text-align: left;
		padding: 0.75rem;
		border-bottom: 2px solid #30363d;
		color: #8b949e;
		font-size: 0.875rem;
		font-weight: 600;
	}

	.proposals-table td {
		padding: 0.75rem;
		border-bottom: 1px solid #21262d;
		color: #c9d1d9;
	}

	.proposals-table tr {
		cursor: pointer;
		transition: background 0.15s;
	}

	.proposals-table tr:hover {
		background: #21262d;
	}

	.proposals-table tr.active {
		background: #1f3d5c;
	}

	/* Badges */
	.badge {
		font-size: 0.75rem;
		padding: 0.25rem 0.75rem;
		border-radius: 12px;
		font-weight: 600;
		text-transform: uppercase;
	}

	.badge-detected { background: #21262d; color: #8b949e; }
	.badge-drafted { background: #1f3d5c; color: #58a6ff; }
	.badge-approved { background: #238636; color: #fff; }
	.badge-rejected { background: #da3633; color: #fff; }
	.badge-archived { background: #161b22; color: #6e7681; }

	.risk-high { background: #b62324; color: #fff; }
	.risk-medium { background: #9e6a03; color: #ffa657; }
	.risk-low { background: #238636; color: #fff; }

	.severity-critical { background: #b62324; color: #fff; }
	.severity-high { background: #9e6a03; color: #ffa657; }
	.severity-medium { background: #238636; color: #fff; }
	.severity-low { background: #21262d; color: #8b949e; }

	.type-badge {
		font-size: 0.75rem;
		background: #2d1f3d;
		color: #bc8cff;
		padding: 0.25rem 0.75rem;
		border-radius: 12px;
	}

	/* Detail Section */
	.detail-section {
		border: 2px solid #58a6ff;
	}

	.detail-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 1.5rem;
		padding-bottom: 1rem;
		border-bottom: 1px solid #30363d;
	}

	.close-btn {
		background: transparent;
		border: 1px solid #30363d;
		border-radius: 6px;
		color: #8b949e;
		cursor: pointer;
		font-size: 1.2rem;
		padding: 0.25rem 0.5rem;
	}

	.close-btn:hover {
		background: #21262d;
		color: #c9d1d9;
	}

	.detail-title h3 {
		font-size: 1.5rem;
		margin: 0 0 0.75rem 0;
		color: #c9d1d9;
	}

	.detail-title .badges {
		display: flex;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.detail-meta {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 1rem;
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 1rem;
		margin-bottom: 1.5rem;
	}

	.meta-item {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}

	.meta-item .label {
		font-size: 0.75rem;
		color: #8b949e;
		font-weight: 600;
	}

	.meta-item .value {
		font-size: 0.875rem;
		color: #c9d1d9;
	}

	.confidence-bar {
		position: relative;
		width: 100%;
		height: 6px;
		background: #30363d;
		border-radius: 3px;
		overflow: hidden;
	}

	.confidence-fill {
		height: 100%;
		transition: width 0.3s;
	}

	.confidence-value {
		font-size: 0.875rem;
		font-weight: 700;
	}

	.detail-block {
		margin-bottom: 1.5rem;
	}

	.detail-block h4 {
		font-size: 1rem;
		margin: 0 0 0.75rem 0;
		color: #58a6ff;
	}

	.detail-block p {
		color: #c9d1d9;
		line-height: 1.6;
		margin: 0;
	}

	.decision-reason {
		font-style: italic;
		color: #ffa657;
	}

	.json-block {
		background: #0d1117;
		border: 1px solid #21262d;
		border-radius: 6px;
		padding: 1rem;
		overflow-x: auto;
		font-family: monospace;
		font-size: 0.875rem;
		color: #c9d1d9;
		margin: 0;
	}

	.detail-actions {
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 1rem;
	}

	.detail-actions h4 {
		font-size: 1rem;
		margin: 0 0 0.75rem 0;
		color: #58a6ff;
	}

	.reason-input {
		margin-bottom: 1rem;
	}

	.reason-input label {
		display: block;
		margin-bottom: 0.5rem;
		color: #c9d1d9;
		font-weight: 600;
	}

	.reason-input textarea {
		width: 100%;
		background: #0d1117;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 0.75rem;
		color: #c9d1d9;
		font-family: inherit;
		font-size: 0.9rem;
		resize: vertical;
	}

	.action-buttons {
		display: flex;
		gap: 0.75rem;
		flex-wrap: wrap;
	}

	.action-btn {
		padding: 0.75rem 1.5rem;
		border-radius: 6px;
		border: none;
		font-size: 0.9rem;
		font-weight: 600;
		cursor: pointer;
		transition: all 0.15s;
	}

	.action-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.action-btn.drafted {
		background: #1f3d5c;
		color: #58a6ff;
	}

	.action-btn.drafted:hover:not(:disabled) {
		background: #264f73;
	}

	.action-btn.approved {
		background: #238636;
		color: #fff;
	}

	.action-btn.approved:hover:not(:disabled) {
		background: #2ea043;
	}

	.action-btn.rejected {
		background: #da3633;
		color: #fff;
	}

	.action-btn.rejected:hover:not(:disabled) {
		background: #f85149;
	}

	.action-btn.archived {
		background: #21262d;
		color: #8b949e;
		border: 1px solid #30363d;
	}

	.action-btn.archived:hover:not(:disabled) {
		background: #30363d;
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

	/* Scorecards Section */
	.scorecards-section {
		background: #161b22;
		border: 1px solid #30363d;
		border-radius: 8px;
		padding: 1.5rem;
	}

	.scorecards-section h2 {
		margin: 0 0 1.5rem 0;
		font-size: 1.5rem;
		color: #58a6ff;
	}

	.section-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 1.5rem;
	}

	.section-header h2 {
		margin: 0;
	}

	.scorecard-controls {
		display: flex;
		gap: 0.75rem;
		align-items: center;
	}

	.scorecard-controls select {
		padding: 0.5rem 1rem;
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		color: #c9d1d9;
		font-size: 0.9rem;
		cursor: pointer;
	}

	.compute-btn {
		padding: 0.5rem 1rem;
		background: #238636;
		border: 1px solid #238636;
		border-radius: 6px;
		color: #fff;
		font-size: 0.875rem;
		cursor: pointer;
		transition: all 0.15s;
	}

	.compute-btn:hover:not(:disabled) {
		background: #2ea043;
	}

	.compute-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.highlights-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 1rem;
		margin-bottom: 1.5rem;
	}

	.highlight-card {
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 1rem;
		text-align: center;
	}

	.highlight-card.positive {
		border-color: #238636;
		background: rgba(35, 134, 54, 0.1);
	}

	.highlight-card.negative {
		border-color: #da3633;
		background: rgba(218, 54, 51, 0.1);
	}

	.highlight-card.warning {
		border-color: #9e6a03;
		background: rgba(158, 106, 3, 0.1);
	}

	.highlight-label {
		font-size: 0.75rem;
		color: #8b949e;
		text-transform: uppercase;
		margin-bottom: 0.5rem;
	}

	.highlight-value {
		font-size: 1.25rem;
		font-weight: 600;
		color: #c9d1d9;
		margin-bottom: 0.25rem;
	}

	.highlight-detail {
		font-size: 0.875rem;
		color: #8b949e;
	}

	.scorecards-table {
		width: 100%;
		border-collapse: collapse;
	}

	.scorecards-table th {
		text-align: left;
		padding: 0.75rem;
		border-bottom: 2px solid #30363d;
		color: #8b949e;
		font-size: 0.875rem;
		font-weight: 600;
	}

	.scorecards-table td {
		padding: 0.75rem;
		border-bottom: 1px solid #21262d;
		color: #c9d1d9;
	}

	.scorecards-table tr:hover {
		background: #21262d;
	}

	.agent-name, .workflow-type {
		font-weight: 600;
		color: #58a6ff;
	}

	.trend-badge {
		font-size: 0.75rem;
		padding: 0.25rem 0.5rem;
		border-radius: 4px;
		text-transform: capitalize;
	}

	.trend-improving {
		background: rgba(63, 185, 80, 0.15);
	}

	.trend-degrading {
		background: rgba(248, 81, 73, 0.15);
	}

	.trend-stable {
		background: rgba(139, 148, 158, 0.15);
	}

	/* Routing Recommendations Section */
	.routing-section {
		background: #161b22;
		border: 1px solid #30363d;
		border-radius: 8px;
		padding: 1.5rem;
	}

	.routing-section h2 {
		margin: 0 0 1.5rem 0;
		font-size: 1.5rem;
		color: #58a6ff;
	}

	.analyze-btn {
		padding: 0.5rem 1rem;
		background: #238636;
		border: 1px solid #238636;
		border-radius: 6px;
		color: #fff;
		font-size: 0.875rem;
		cursor: pointer;
		transition: all 0.15s;
	}

	.analyze-btn:hover:not(:disabled) {
		background: #2ea043;
	}

	.analyze-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.routing-table {
		width: 100%;
		border-collapse: collapse;
	}

	.routing-table th {
		text-align: left;
		padding: 0.75rem;
		border-bottom: 2px solid #30363d;
		color: #8b949e;
		font-size: 0.875rem;
		font-weight: 600;
	}

	.routing-table td {
		padding: 0.75rem;
		border-bottom: 1px solid #21262d;
		color: #c9d1d9;
	}

	.routing-table tr {
		cursor: pointer;
		transition: background 0.15s;
	}

	.routing-table tr:hover {
		background: #21262d;
	}

	.routing-table tr.active {
		background: #1f3d5c;
	}

	.workflow-name {
		font-weight: 600;
		color: #58a6ff;
	}

	.reason-cell {
		max-width: 300px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.type-badge.routing-best_agent {
		background: #238636;
		color: #fff;
	}

	.type-badge.routing-deprioritize {
		background: #da3633;
		color: #fff;
	}

	.type-badge.routing-fallback_needed {
		background: #9e6a03;
		color: #ffa657;
	}

	.type-badge.routing-instability {
		background: #b62324;
		color: #fff;
	}

	.routing-detail {
		margin-top: 1.5rem;
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 8px;
		padding: 1.5rem;
	}

	.routing-detail .detail-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 1rem;
	}

	.routing-detail h3 {
		margin: 0;
		font-size: 1.25rem;
		color: #58a6ff;
	}

	.metrics-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 1rem;
	}

	.metric-item {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}

	.metric-label {
		font-size: 0.75rem;
		color: #8b949e;
		text-transform: uppercase;
	}

	.metric-value {
		font-size: 1.125rem;
		font-weight: 600;
		color: #c9d1d9;
	}
</style>
