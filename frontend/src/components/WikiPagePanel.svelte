<script lang="ts">
	import { renderMarkdown } from '$lib/markdown'
	import type { WikiPage, WikiLink } from '$lib/types'

	interface Props {
		page: WikiPage | null
		linksFrom: WikiLink[]
		linksTo: WikiLink[]
		allPages: WikiPage[]
		onClose: () => void
		onNavigate: (pageId: string) => void
	}

	let { page, linksFrom, linksTo, allPages, onClose, onNavigate }: Props = $props()

	const renderedContent = $derived(page ? renderMarkdown(page.content) : '')

	const isStale = $derived(page ? page.staleness_score > 0.7 : false)

	function stalenessLabel(score: number): string {
		if (score >= 0.8) return 'very stale'
		if (score >= 0.7) return 'stale'
		if (score >= 0.5) return 'aging'
		return 'fresh'
	}

	function stalenessColor(score: number): string {
		if (score >= 0.8) return '#f85149'
		if (score >= 0.5) return '#d29922'
		if (score >= 0.2) return '#3fb950'
		return '#58a6ff'
	}

	function formatDate(ts: string) {
		if (!ts) return '—'
		return new Date(ts).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
	}

	function pageTitle(id: string): string {
		const p = allPages.find((p) => p.id === id)
		return p ? p.title : id.slice(0, 12) + '…'
	}

	function handleKeyDown(e: KeyboardEvent) {
		if (e.key === 'Escape') onClose()
	}
</script>

<svelte:window onkeydown={handleKeyDown} />

{#if page}
	<aside class="wiki-panel" aria-label="Wiki page detail">
		<div class="panel-header">
			<h2 class="panel-title">{page.title}</h2>
			<button class="close-btn" onclick={onClose} aria-label="Close panel">✕</button>
		</div>

		<div class="panel-badges">
			<span class="badge type-{page.page_type}">{page.page_type}</span>
			<span class="badge status-{page.status}">{page.status}</span>
			{#if isStale}
				<span
					class="badge badge-stale-warn"
					title="Staleness: {(page.staleness_score * 100).toFixed(0)}%"
					style="color: {stalenessColor(page.staleness_score)}"
				>
					⚠ {stalenessLabel(page.staleness_score)}
				</span>
			{/if}
		</div>

		<div class="panel-meta">
			<span>v{page.version}</span>
			<span>Updated {formatDate(page.updated_at)}</span>
			{#if page.generated_by}
				<span>by {page.generated_by}</span>
			{/if}
		</div>

		<div class="panel-body">
			{#if renderedContent}
				<div class="markdown-content">
					{@html renderedContent}
				</div>
			{:else}
				<p class="no-content">No content available.</p>
			{/if}
		</div>

		{#if linksFrom.length > 0 || linksTo.length > 0}
			<div class="panel-links">
				{#if linksFrom.length > 0}
					<div class="links-section">
						<h3 class="links-heading">Links from this page</h3>
						<div class="link-chips">
							{#each linksFrom as link}
								<button
									class="link-chip"
									onclick={() => onNavigate(link.to_page_id)}
									title="Type: {link.link_type}"
								>
									<span class="link-type-dot link-type-{link.link_type}"></span>
									{pageTitle(link.to_page_id)}
								</button>
							{/each}
						</div>
					</div>
				{/if}

				{#if linksTo.length > 0}
					<div class="links-section">
						<h3 class="links-heading">Links to this page</h3>
						<div class="link-chips">
							{#each linksTo as link}
								<button
									class="link-chip"
									onclick={() => onNavigate(link.from_page_id)}
									title="Type: {link.link_type}"
								>
									<span class="link-type-dot link-type-{link.link_type}"></span>
									{pageTitle(link.from_page_id)}
								</button>
							{/each}
						</div>
					</div>
				{/if}
			</div>
		{/if}
	</aside>
{/if}

<style>
	.wiki-panel {
		position: sticky;
		top: 0;
		width: 460px;
		min-width: 320px;
		height: 100vh;
		overflow-y: auto;
		background: #161b22;
		border-left: 1px solid #30363d;
		display: flex;
		flex-direction: column;
		flex-shrink: 0;
	}

	.panel-header {
		display: flex;
		align-items: flex-start;
		gap: 0.75rem;
		padding: 1.25rem 1.25rem 0.75rem;
		border-bottom: 1px solid #30363d;
		position: sticky;
		top: 0;
		background: #161b22;
		z-index: 1;
	}

	.panel-title {
		flex: 1;
		font-size: 1.1rem;
		font-weight: 600;
		color: #c9d1d9;
		margin: 0;
		line-height: 1.4;
		word-break: break-word;
	}

	.close-btn {
		flex-shrink: 0;
		background: none;
		border: none;
		color: #8b949e;
		cursor: pointer;
		font-size: 1rem;
		padding: 0.125rem 0.25rem;
		border-radius: 4px;
		line-height: 1;
		transition: color 0.15s, background 0.15s;
	}
	.close-btn:hover {
		color: #c9d1d9;
		background: #30363d;
	}

	.panel-badges {
		display: flex;
		gap: 0.375rem;
		flex-wrap: wrap;
		padding: 0.75rem 1.25rem 0;
	}

	.badge {
		font-size: 0.7rem;
		padding: 2px 7px;
		border-radius: 10px;
		font-weight: 500;
		white-space: nowrap;
		border: 1px solid transparent;
	}

	/* Type badges */
	.type-summary { background: #1c2a3a; color: #58a6ff; border-color: #1f6feb; }
	.type-entity  { background: #2a1c3a; color: #bc8cff; border-color: #8957e5; }
	.type-concept { background: #1a2d2a; color: #56d364; border-color: #2ea043; }
	.type-answer  { background: #2d2208; color: #ffa657; border-color: #9e6a03; }
	.type-index   { background: #21262d; color: #8b949e; border-color: #30363d; }

	/* Status badges */
	.status-published { background: #1a3a2a; color: #3fb950; border-color: #2ea043; }
	.status-draft     { background: #21262d; color: #8b949e; border-color: #30363d; }
	.status-stale     { background: #2d2208; color: #d29922; border-color: #9e6a03; }
	.status-archived  { background: #161b22; color: #6e7681; border-color: #30363d; }

	.badge-stale-warn {
		background: #21262d;
		border-color: #30363d;
		font-size: 0.7rem;
	}

	.panel-meta {
		display: flex;
		gap: 0.75rem;
		flex-wrap: wrap;
		padding: 0.5rem 1.25rem 0.75rem;
		font-size: 0.75rem;
		color: #6e7681;
		border-bottom: 1px solid #30363d;
	}

	.panel-body {
		flex: 1;
		padding: 1rem 1.25rem;
		overflow-y: auto;
	}

	.no-content {
		color: #6e7681;
		font-size: 0.875rem;
		font-style: italic;
	}

	/* Markdown rendered content */
	.markdown-content {
		font-size: 0.875rem;
		color: #c9d1d9;
		line-height: 1.7;
	}

	.markdown-content :global(h1),
	.markdown-content :global(h2),
	.markdown-content :global(h3),
	.markdown-content :global(h4) {
		color: #e6edf3;
		margin: 1.25em 0 0.5em;
		font-weight: 600;
	}

	.markdown-content :global(h1) { font-size: 1.2rem; }
	.markdown-content :global(h2) { font-size: 1.05rem; }
	.markdown-content :global(h3) { font-size: 0.95rem; }
	.markdown-content :global(h4) { font-size: 0.875rem; }

	.markdown-content :global(p) {
		margin: 0 0 0.875em;
	}

	.markdown-content :global(ul),
	.markdown-content :global(ol) {
		margin: 0 0 0.875em;
		padding-left: 1.5em;
	}

	.markdown-content :global(li) {
		margin-bottom: 0.25em;
	}

	.markdown-content :global(code) {
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 4px;
		padding: 0.1em 0.35em;
		font-size: 0.8em;
		font-family: 'Consolas', 'Monaco', monospace;
		color: #e6edf3;
	}

	.markdown-content :global(pre) {
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 6px;
		padding: 0.875rem 1rem;
		overflow-x: auto;
		margin: 0 0 0.875em;
	}

	.markdown-content :global(pre code) {
		background: none;
		border: none;
		padding: 0;
		font-size: 0.8rem;
	}

	.markdown-content :global(blockquote) {
		border-left: 3px solid #30363d;
		padding-left: 1rem;
		color: #8b949e;
		margin: 0 0 0.875em;
	}

	.markdown-content :global(a) {
		color: #58a6ff;
		text-decoration: none;
	}
	.markdown-content :global(a:hover) {
		text-decoration: underline;
	}

	.markdown-content :global(table) {
		border-collapse: collapse;
		width: 100%;
		margin: 0 0 0.875em;
		font-size: 0.8rem;
	}

	.markdown-content :global(th),
	.markdown-content :global(td) {
		border: 1px solid #30363d;
		padding: 0.4rem 0.75rem;
		text-align: left;
	}

	.markdown-content :global(th) {
		background: #21262d;
		color: #c9d1d9;
		font-weight: 600;
	}

	.markdown-content :global(hr) {
		border: none;
		border-top: 1px solid #30363d;
		margin: 1em 0;
	}

	/* Links section */
	.panel-links {
		padding: 0.875rem 1.25rem 1.25rem;
		border-top: 1px solid #30363d;
		display: flex;
		flex-direction: column;
		gap: 0.875rem;
	}

	.links-section {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}

	.links-heading {
		font-size: 0.75rem;
		font-weight: 600;
		color: #8b949e;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		margin: 0;
	}

	.link-chips {
		display: flex;
		flex-wrap: wrap;
		gap: 0.375rem;
	}

	.link-chip {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		padding: 3px 9px;
		background: #21262d;
		border: 1px solid #30363d;
		border-radius: 10px;
		color: #c9d1d9;
		font-size: 0.75rem;
		cursor: pointer;
		transition: background 0.15s, border-color 0.15s;
	}
	.link-chip:hover {
		background: #30363d;
		border-color: #58a6ff;
		color: #58a6ff;
	}

	.link-type-dot {
		display: inline-block;
		width: 7px;
		height: 7px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.link-type-related    { background: #6b7280; }
	.link-type-parent     { background: #3b82f6; }
	.link-type-child      { background: #10b981; }
	.link-type-contradicts { background: #ef4444; }
	.link-type-supersedes { background: #f97316; }
	.link-type-cites      { background: #a855f7; }
</style>
