<script lang="ts">
  import { onMount } from 'svelte'
  import {
    listAgents, listSkills, createAgent, updateAgent, deleteAgent,
    assignSkills, createSkill, updateSkill, deleteSkill,
    listRules, createRule, updateRule, deleteRule
  } from '$lib/api'
  import type { AgentDef, SkillDef, AgentsResponse, SkillsResponse, RuleDef, RulesResponse } from '$lib/types'

  let agents = $state<AgentsResponse | null>(null)
  let skills = $state<SkillDef[]>([])
  let loading = $state(true)
  let error = $state<string | null>(null)
  let activeView = $state<'agents' | 'skills' | 'rules'>('agents')
  let selectedAgent = $state<string | null>(null)
  let showCreateAgent = $state(false)
  let showCreateSkill = $state(false)
  let showSkillAssignment = $state(false)
  let confirmDelete = $state<string | null>(null)
  let confirmDeleteType = $state<'agent' | 'skill' | 'rule'>('agent')

  let newAgentName = $state('')
  let newAgentDesc = $state('')
  let newAgentModel = $state('sonnet')
  let newAgentTools = $state('Read, Grep, Glob, Edit, Write, Bash')
  let newAgentBody = $state('')

  let newSkillName = $state('')
  let newSkillDesc = $state('')
  let newSkillBody = $state('')

  let editAgent = $state<{
    name: string
    description: string
    model: string
    tools: string[]
    skills: string[]
    body: string
  } | null>(null)

  let editSkill = $state<{
    name: string
    description: string
    disable_model_invocation: boolean
    argument_hint: string
    body: string
  } | null>(null)

  let skillSearch = $state('')

  let rules = $state<RuleDef[]>([])
  let showCreateRule = $state(false)
  let newRuleName = $state('')
  let newRuleTitle = $state('')
  let newRuleBody = $state('')
  let editRule = $state<{ name: string; title: string; body: string } | null>(null)
  let ruleSearch = $state('')

  let selectedSkillsForAssignment = $state<string[]>([])
  let agentFilter = $state<'all' | 'claude_code' | 'opencode'>('all')

  let showSkillPicker = $state(false)
  let showSkillCreatorLauncher = $state(false)
  let skillCreatorTarget = $state<'claude_code' | 'opencode'>('claude_code')
  let skillCreatorCopied = $state(false)

  function copySkillCreatorCommand() {
    const cmd = skillCreatorTarget === 'claude_code' ? 'claude /skill-creator' : 'opencode /skill-creator'
    navigator.clipboard.writeText(cmd)
    skillCreatorCopied = true
    setTimeout(() => { skillCreatorCopied = false }, 2000)
  }

  let showAgentPicker = $state(false)
  let showAgentCreatorLauncher = $state(false)
  let agentCreatorTarget = $state<'claude_code' | 'opencode'>('claude_code')
  let agentCreatorCopied = $state(false)

  function copyAgentCreatorCommand() {
    const cmd = agentCreatorTarget === 'claude_code' ? 'claude /create-agent' : 'opencode /create-agent'
    navigator.clipboard.writeText(cmd)
    agentCreatorCopied = true
    setTimeout(() => { agentCreatorCopied = false }, 2000)
  }

  let allAgentNames = $derived.by(() => {
    if (!agents) return []
    const names = new Set<string>()
    agents.claude_code?.forEach(a => names.add(a.name))
    agents.opencode?.forEach(a => names.add(a.name))
    return Array.from(names).sort()
  })

  let filteredAgentNames = $derived.by(() => {
    if (agentFilter === 'all') return allAgentNames
    if (agentFilter === 'claude_code') return allAgentNames.filter(n => !!getAgentDef(n, 'claude_code'))
    return allAgentNames.filter(n => !!getAgentDef(n, 'opencode'))
  })

  let filteredSkills = $derived.by(() => {
    if (!skillSearch) return skills
    const q = skillSearch.toLowerCase()
    return skills.filter(s =>
      s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q)
    )
  })

  let filteredRules = $derived.by(() => {
    if (!ruleSearch) return rules
    const q = ruleSearch.toLowerCase()
    return rules.filter(r =>
      r.name.toLowerCase().includes(q) || r.title.toLowerCase().includes(q)
    )
  })

  function getAgentDef(name: string, format: 'claude_code' | 'opencode'): AgentDef | undefined {
    if (!agents) return undefined
    const list = format === 'claude_code' ? agents.claude_code : agents.opencode
    return list?.find(a => a.name === name)
  }

  function getSkillNamesForAgent(name: string): string[] {
    const cc = getAgentDef(name, 'claude_code')
    const oc = getAgentDef(name, 'opencode')
    const ccSkills = cc?.skills ?? []
    const ocSkills = oc?.skills ?? []
    return Array.from(new Set([...ccSkills, ...ocSkills])).sort()
  }

  async function loadData() {
    try {
      const [agentsData, skillsData, rulesData] = await Promise.all([listAgents(), listSkills(), listRules()])
      agents = agentsData
      skills = skillsData?.skills ?? []
      rules = rulesData?.rules ?? []
      error = null
    } catch (e: any) {
      console.error('Failed to load data:', e)
      error = e.message || 'Failed to load data'
      if (!agents) {
        agents = { claude_code: [], opencode: [] }
      }
    } finally {
      loading = false
    }
  }

  onMount(loadData)

  function navigateToSkill(skill: string) {
    activeView = 'skills'
    skillSearch = skill
  }

  async function handleCreateAgent() {
    if (!newAgentName.trim()) return
    try {
      await createAgent({
        name: newAgentName.trim(),
        description: newAgentDesc.trim(),
        model: newAgentModel,
        tools: newAgentTools.split(',').map(t => t.trim()).filter(Boolean),
        body: newAgentBody.trim() || undefined
      })
      showCreateAgent = false
      newAgentName = ''
      newAgentDesc = ''
      newAgentModel = 'sonnet'
      newAgentTools = 'Read, Grep, Glob, Edit, Write, Bash'
      newAgentBody = ''
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }

  async function handleCreateSkill() {
    if (!newSkillName.trim()) return
    try {
      await createSkill({
        name: newSkillName.trim(),
        description: newSkillDesc.trim(),
        body: newSkillBody.trim() || undefined
      })
      showCreateSkill = false
      newSkillName = ''
      newSkillDesc = ''
      newSkillBody = ''
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }

  async function handleDeleteAgent(name: string) {
    if (confirmDelete !== name || confirmDeleteType !== 'agent') {
      confirmDelete = name
      confirmDeleteType = 'agent'
      setTimeout(() => { if (confirmDelete === name && confirmDeleteType === 'agent') confirmDelete = null }, 5000)
      return
    }
    try {
      await deleteAgent(name)
      confirmDelete = null
      if (selectedAgent === name) selectedAgent = null
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }

  async function handleDeleteSkill(name: string) {
    if (confirmDelete !== name || confirmDeleteType !== 'skill') {
      confirmDelete = name
      confirmDeleteType = 'skill'
      setTimeout(() => { if (confirmDelete === name && confirmDeleteType === 'skill') confirmDelete = null }, 5000)
      return
    }
    try {
      await deleteSkill(name)
      confirmDelete = null
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }

  function startEditAgent(name: string) {
    const cc = getAgentDef(name, 'claude_code')
    const oc = getAgentDef(name, 'opencode')
    const primary = cc ?? oc
    if (!primary) return
    editAgent = {
      name: primary.name,
      description: primary.description,
      model: primary.model || 'sonnet',
      tools: primary.tools,
      skills: getSkillNamesForAgent(name),
      body: primary.body
    }
  }

  function startEditSkill(skill: SkillDef) {
    editSkill = {
      name: skill.name,
      description: skill.description,
      disable_model_invocation: skill.disable_model_invocation,
      argument_hint: skill.argument_hint || '',
      body: skill.body
    }
  }

  async function handleUpdateAgent() {
    if (!editAgent) return
    try {
      await updateAgent(editAgent.name, {
        description: editAgent.description,
        model: editAgent.model,
        tools: editAgent.tools,
        body: editAgent.body
      })
      editAgent = null
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }

  async function handleUpdateSkill() {
    if (!editSkill) return
    try {
      await updateSkill(editSkill.name, {
        description: editSkill.description,
        disable_model_invocation: editSkill.disable_model_invocation,
        argument_hint: editSkill.argument_hint,
        body: editSkill.body
      })
      editSkill = null
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }

  function startSkillAssignment(name: string) {
    selectedAgent = name
    selectedSkillsForAssignment = getSkillNamesForAgent(name)
    showSkillAssignment = true
  }

  async function handleAssignSkills() {
    if (!selectedAgent) return
    try {
      await assignSkills(selectedAgent, selectedSkillsForAssignment)
      showSkillAssignment = false
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }

  function toggleSkillAssignment(skillName: string) {
    if (selectedSkillsForAssignment.includes(skillName)) {
      selectedSkillsForAssignment = selectedSkillsForAssignment.filter(s => s !== skillName)
    } else {
      selectedSkillsForAssignment = [...selectedSkillsForAssignment, skillName]
    }
  }

  async function handleCreateRule() {
    if (!newRuleName.trim()) return
    try {
      await createRule({
        name: newRuleName.trim(),
        title: newRuleTitle.trim() || undefined,
        body: newRuleBody.trim() || undefined
      })
      showCreateRule = false
      newRuleName = ''
      newRuleTitle = ''
      newRuleBody = ''
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }

  async function handleDeleteRule(name: string) {
    if (confirmDelete !== name || confirmDeleteType !== 'rule') {
      confirmDelete = name
      confirmDeleteType = 'rule'
      setTimeout(() => { if (confirmDelete === name && confirmDeleteType === 'rule') confirmDelete = null }, 5000)
      return
    }
    try {
      await deleteRule(name)
      confirmDelete = null
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }

  function startEditRule(rule: RuleDef) {
    editRule = { name: rule.name, title: rule.title, body: rule.body }
  }

  async function handleUpdateRule() {
    if (!editRule) return
    try {
      await updateRule(editRule.name, {
        title: editRule.title,
        body: editRule.body
      })
      editRule = null
      await loadData()
    } catch (e: any) {
      error = e.message
    }
  }
</script>

<div class="agents-page">
  {#if loading}
    <div class="loading">Loading agents, skills & rules…</div>
  {:else if error && !agents}
    <div class="error">Error: {error}</div>
  {:else}
    {#if error}
      <div class="error-banner">
        Error: {error}
        <button class="btn-sm" onclick={() => (error = null)}>Dismiss</button>
      </div>
    {/if}
    <div class="toolbar">
      <div class="view-toggle">
        <button class:active={activeView === 'agents'} onclick={() => (activeView = 'agents')}>
          Agents ({allAgentNames.length})
        </button>
        <button class:active={activeView === 'skills'} onclick={() => (activeView = 'skills')}>
          Skills ({skills.length})
        </button>
        <button class:active={activeView === 'rules'} onclick={() => (activeView = 'rules')}>
          Rules ({rules.length})
        </button>
      </div>
      {#if activeView === 'agents'}
        <div class="format-filter">
          <button class:active={agentFilter === 'all'} onclick={() => (agentFilter = 'all')}>
            All ({allAgentNames.length})
          </button>
          <button class:active={agentFilter === 'claude_code'} onclick={() => (agentFilter = 'claude_code')}>
            <span class="badge cc">CC</span> Claude Code
          </button>
          <button class:active={agentFilter === 'opencode'} onclick={() => (agentFilter = 'opencode')}>
            <span class="badge oc">OC</span> OpenCode
          </button>
        </div>
      {/if}
      <div class="actions">
        {#if activeView === 'agents'}
          <button class="btn-primary" onclick={() => (showAgentPicker = true)}>+ New Agent</button>
        {:else if activeView === 'skills'}
          <button class="btn-primary" onclick={() => (showSkillPicker = true)}>+ New Skill</button>
        {:else}
          <button class="btn-primary" onclick={() => (showCreateRule = true)}>+ New Rule</button>
        {/if}
      </div>
    </div>

    <!-- AGENTS VIEW -->
    <div hidden={activeView !== 'agents'}>
      <div class="agents-grid">
        {#each filteredAgentNames as name (name)}
          {@const ccAgent = getAgentDef(name, 'claude_code')}
          {@const ocAgent = getAgentDef(name, 'opencode')}
          {@const primary = ccAgent ?? ocAgent}
          {@const agentSkills = getSkillNamesForAgent(name)}
          {@const isConfirm = confirmDelete === name && confirmDeleteType === 'agent'}
          <div class="agent-card" class:selected={selectedAgent === name}>
            <div class="card-header">
              <div class="card-title">
                <span class="agent-name">{name}</span>
                <div class="format-badges">
                  {#if ccAgent}
                    <span class="badge cc" title="Claude Code">CC</span>
                  {/if}
                  {#if ocAgent}
                    <span class="badge oc" title="OpenCode">OC</span>
                  {/if}
                  {#if ccAgent?.model}
                    <span class="badge model" title="Claude Code model">{ccAgent.model}</span>
                  {/if}
                  {#if ocAgent?.model && ocAgent.model !== ccAgent?.model}
                    <span class="badge model oc-model" title="OpenCode model">{ocAgent.model}</span>
                  {/if}
                </div>
              </div>
              <div class="card-actions">
                <button class="btn-sm" onclick={() => startEditAgent(name)} title="Edit">✎</button>
                <button class="btn-sm" onclick={() => startSkillAssignment(name)} title="Assign Skills">⚡</button>
                <button
                  class="btn-sm btn-danger"
                  class:confirming={isConfirm}
                  onclick={() => handleDeleteAgent(name)}
                >
                  {isConfirm ? 'Confirm?' : '✕'}
                </button>
              </div>
            </div>
            <div class="card-description">{primary?.description || ''}</div>
            {#if primary?.tools && primary.tools.length > 0}
              <div class="card-tools">
                {#each primary.tools.slice(0, 6) as tool}
                  <span class="tool-tag">{tool}</span>
                {/each}
                {#if primary.tools.length > 6}
                  <span class="tool-tag more">+{primary.tools.length - 6}</span>
                {/if}
              </div>
            {/if}
            {#if agentSkills.length > 0}
              <div class="card-skills">
                {#each agentSkills as skill}
                  <button type="button" class="skill-chip" onclick={() => navigateToSkill(skill)}>{skill}</button>
                {/each}
              </div>
            {:else}
              <div class="card-skills empty">No skills assigned</div>
            {/if}
          </div>
        {/each}
      </div>
    </div>

    <!-- SKILLS VIEW -->
    <div hidden={activeView !== 'skills'}>
      <div class="skills-header">
        <input
          type="text"
          class="search-input"
          placeholder="Search skills..."
          bind:value={skillSearch}
        />
      </div>
      <div class="skills-grid">
        {#each filteredSkills as skill (skill.name)}
          {@const isConfirm = confirmDelete === skill.name && confirmDeleteType === 'skill'}
          <div class="skill-card">
            <div class="card-header">
              <div class="card-title">
                <span class="skill-name">{skill.name}</span>
                {#if skill.disable_model_invocation}
                  <span class="badge locked" title="Model invocation disabled">🔒</span>
                {/if}
                {#if skill.has_resources}
                  <span class="badge resources" title="Has bundled resources">📦</span>
                {/if}
              </div>
              <div class="card-actions">
                <button class="btn-sm" onclick={() => startEditSkill(skill)} title="Edit">✎</button>
                <button
                  class="btn-sm btn-danger"
                  class:confirming={isConfirm}
                  onclick={() => handleDeleteSkill(skill.name)}
                >
                  {isConfirm ? 'Confirm?' : '✕'}
                </button>
              </div>
            </div>
            <div class="card-description">{skill.description}</div>
            {#if skill.argument_hint}
              <div class="card-hint">Argument: <code>{skill.argument_hint}</code></div>
            {/if}
            {#if skill.resource_dirs?.length > 0}
              <div class="card-resources">
                {#each skill.resource_dirs as dir}
                  <span class="resource-tag">{dir}/</span>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </div>

    <!-- RULES VIEW -->
    <div hidden={activeView !== 'rules'}>
      <div class="skills-header">
        <input
          type="text"
          class="search-input"
          placeholder="Search rules..."
          bind:value={ruleSearch}
        />
      </div>
      <div class="skills-grid">
        {#each filteredRules as rule (rule.name)}
          {@const isConfirm = confirmDelete === rule.name && confirmDeleteType === 'rule'}
          <div class="skill-card">
            <div class="card-header">
              <div class="card-title">
                <span class="skill-name">{rule.title || rule.name}</span>
                <span class="badge rule-badge">{rule.name}</span>
              </div>
              <div class="card-actions">
                <button class="btn-sm" onclick={() => startEditRule(rule)} title="Edit">✎</button>
                <button
                  class="btn-sm btn-danger"
                  class:confirming={isConfirm}
                  onclick={() => handleDeleteRule(rule.name)}
                >
                  {isConfirm ? 'Confirm?' : '✕'}
                </button>
              </div>
            </div>
            <div class="card-description rule-preview">
              {(rule.body ?? '').split('\n').filter((l) => l.trim() && !l.startsWith('#')).slice(0, 3).join('\n') || 'No preview available'}
            </div>
          </div>
        {/each}
        {#if filteredRules.length === 0}
          <div class="empty-state">{ruleSearch ? 'No rules match your search.' : 'No rules yet. Create one to get started.'}</div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<!-- Agent Creation Picker -->
{#if showAgentPicker}
  <div class="modal-overlay" onclick={() => (showAgentPicker = false)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Create New Agent</h3>
      <div class="skill-create-options">
        <button class="create-option" onclick={() => { showAgentPicker = false; showAgentCreatorLauncher = true }}>
          <div class="option-icon">✨</div>
          <div class="option-label">AI-assisted</div>
          <div class="option-desc">Use <code>create-agent</code> skill via Claude Code or OpenCode</div>
        </button>
        <button class="create-option" onclick={() => { showAgentPicker = false; showCreateAgent = true }}>
          <div class="option-icon">✏️</div>
          <div class="option-label">Manual</div>
          <div class="option-desc">Fill in the agent form manually (creates both CC + OC)</div>
        </button>
      </div>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (showAgentPicker = false)}>Cancel</button>
      </div>
    </div>
  </div>
{/if}

<!-- Agent Creator Launcher -->
{#if showAgentCreatorLauncher}
  <div class="modal-overlay" onclick={() => (showAgentCreatorLauncher = false)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Launch create-agent</h3>
      <p class="launcher-desc">Run the command below in your project directory. The <code>create-agent</code> skill will guide you through naming, tools, skills, and body — and write both Claude Code and OpenCode formats.</p>
      <div class="target-toggle">
        <button class:active={agentCreatorTarget === 'claude_code'} onclick={() => (agentCreatorTarget = 'claude_code')}>
          <span class="badge cc">CC</span> Claude Code
        </button>
        <button class:active={agentCreatorTarget === 'opencode'} onclick={() => (agentCreatorTarget = 'opencode')}>
          <span class="badge oc">OC</span> OpenCode
        </button>
      </div>
      <div class="command-block">
        <code class="command-text">{agentCreatorTarget === 'claude_code' ? 'claude /create-agent' : 'opencode /create-agent'}</code>
        <button class="btn-sm" onclick={copyAgentCreatorCommand}>
          {agentCreatorCopied ? '✓ Copied' : 'Copy'}
        </button>
      </div>
      <div class="agent-format-note">
        <div class="format-note-row">
          <span class="badge cc">CC</span>
          <span>Creates <code>.claude/agents/&lt;name&gt;.md</code> — name, description, tools (CSV), model, skills list</span>
        </div>
        <div class="format-note-row">
          <span class="badge oc">OC</span>
          <span>Creates <code>.opencode/agents/&lt;name&gt;.md</code> — mode: subagent, boolean tools, Workflow Guard section</span>
        </div>
      </div>
      <p class="launcher-note">After the agent is created, click <strong>Refresh</strong> to see it here.</p>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (showAgentCreatorLauncher = false)}>Close</button>
        <button class="btn-primary" onclick={async () => { await loadData() }}>Refresh Agents</button>
      </div>
    </div>
  </div>
{/if}

<!-- Create Agent Modal -->
{#if showCreateAgent}
  <div class="modal-overlay" onclick={() => (showCreateAgent = false)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Create New Agent</h3>
      <div class="form-group">
        <label>Name</label>
        <input type="text" bind:value={newAgentName} placeholder="e.g. delivery-data-analyst" />
      </div>
      <div class="form-group">
        <label>Description</label>
        <input type="text" bind:value={newAgentDesc} placeholder="What does this agent do?" />
      </div>
      <div class="form-row">
        <div class="form-group">
          <label>Model</label>
          <select bind:value={newAgentModel}>
            <option value="sonnet">Sonnet</option>
            <option value="opus">Opus</option>
          </select>
        </div>
      </div>
      <div class="form-group">
        <label>Tools (comma-separated)</label>
        <input type="text" bind:value={newAgentTools} />
      </div>
      <div class="form-group">
        <label>Body (markdown)</label>
        <textarea bind:value={newAgentBody} rows="6" placeholder="Agent instructions in markdown..."></textarea>
      </div>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (showCreateAgent = false)}>Cancel</button>
        <button class="btn-primary" onclick={handleCreateAgent} disabled={!newAgentName.trim()}>Create</button>
      </div>
    </div>
  </div>
{/if}

<!-- Skill Creation Picker -->
{#if showSkillPicker}
  <div class="modal-overlay" onclick={() => (showSkillPicker = false)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Create New Skill</h3>
      <div class="skill-create-options">
        <button class="create-option" onclick={() => { showSkillPicker = false; showSkillCreatorLauncher = true }}>
          <div class="option-icon">✨</div>
          <div class="option-label">AI-assisted</div>
          <div class="option-desc">Use <code>skill-creator</code> via Claude Code or OpenCode</div>
        </button>
        <button class="create-option" onclick={() => { showSkillPicker = false; showCreateSkill = true }}>
          <div class="option-icon">✏️</div>
          <div class="option-label">Manual</div>
          <div class="option-desc">Fill in the skill form manually</div>
        </button>
      </div>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (showSkillPicker = false)}>Cancel</button>
      </div>
    </div>
  </div>
{/if}

<!-- Skill Creator Launcher -->
{#if showSkillCreatorLauncher}
  <div class="modal-overlay" onclick={() => (showSkillCreatorLauncher = false)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Launch skill-creator</h3>
      <p class="launcher-desc">Run the command below in your project directory. The <code>skill-creator</code> skill will guide you through the process interactively.</p>
      <div class="target-toggle">
        <button class:active={skillCreatorTarget === 'claude_code'} onclick={() => (skillCreatorTarget = 'claude_code')}>
          <span class="badge cc">CC</span> Claude Code
        </button>
        <button class:active={skillCreatorTarget === 'opencode'} onclick={() => (skillCreatorTarget = 'opencode')}>
          <span class="badge oc">OC</span> OpenCode
        </button>
      </div>
      <div class="command-block">
        <code class="command-text">{skillCreatorTarget === 'claude_code' ? 'claude /skill-creator' : 'opencode /skill-creator'}</code>
        <button class="btn-sm" onclick={copySkillCreatorCommand}>
          {skillCreatorCopied ? '✓ Copied' : 'Copy'}
        </button>
      </div>
      <p class="launcher-note">After the skill is created, click <strong>Refresh</strong> to see it here.</p>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (showSkillCreatorLauncher = false)}>Close</button>
        <button class="btn-primary" onclick={async () => { await loadData() }}>Refresh Skills</button>
      </div>
    </div>
  </div>
{/if}

<!-- Create Skill Modal -->
{#if showCreateSkill}
  <div class="modal-overlay" onclick={() => (showCreateSkill = false)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Create New Skill</h3>
      <div class="form-group">
        <label>Name</label>
        <input type="text" bind:value={newSkillName} placeholder="e.g. my-custom-workflow" />
      </div>
      <div class="form-group">
        <label>Description</label>
        <input type="text" bind:value={newSkillDesc} placeholder="When to trigger and what this skill does" />
      </div>
      <div class="form-group">
        <label>Body (markdown)</label>
        <textarea bind:value={newSkillBody} rows="8" placeholder="Skill instructions in markdown..."></textarea>
      </div>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (showCreateSkill = false)}>Cancel</button>
        <button class="btn-primary" onclick={handleCreateSkill} disabled={!newSkillName.trim()}>Create</button>
      </div>
    </div>
  </div>
{/if}

<!-- Edit Agent Modal -->
{#if editAgent}
  <div class="modal-overlay" onclick={() => (editAgent = null)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Edit Agent: {editAgent.name}</h3>
      <div class="form-group">
        <label>Description</label>
        <input type="text" bind:value={editAgent.description} />
      </div>
      <div class="form-row">
        <div class="form-group">
          <label>Model</label>
          <select bind:value={editAgent.model}>
            <option value="sonnet">Sonnet</option>
            <option value="opus">Opus</option>
          </select>
        </div>
      </div>
      <div class="form-group">
        <label>Body (markdown)</label>
        <textarea bind:value={editAgent.body} rows="10"></textarea>
      </div>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (editAgent = null)}>Cancel</button>
        <button class="btn-primary" onclick={handleUpdateAgent}>Save</button>
      </div>
    </div>
  </div>
{/if}

<!-- Edit Skill Modal -->
{#if editSkill}
  <div class="modal-overlay" onclick={() => (editSkill = null)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Edit Skill: {editSkill.name}</h3>
      <div class="form-group">
        <label>Description</label>
        <textarea bind:value={editSkill.description} rows="3"></textarea>
      </div>
      <div class="form-group">
        <label>
          <input type="checkbox" bind:checked={editSkill.disable_model_invocation} />
          Disable model invocation
        </label>
      </div>
      <div class="form-group">
        <label>Argument hint</label>
        <input type="text" bind:value={editSkill.argument_hint} />
      </div>
      <div class="form-group">
        <label>Body (markdown)</label>
        <textarea bind:value={editSkill.body} rows="12"></textarea>
      </div>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (editSkill = null)}>Cancel</button>
        <button class="btn-primary" onclick={handleUpdateSkill}>Save</button>
      </div>
    </div>
  </div>
{/if}

<!-- Skill Assignment Modal -->
{#if showSkillAssignment}
  <div class="modal-overlay" onclick={() => (showSkillAssignment = false)} role="presentation">
    <div class="modal modal-wide" onclick={(e) => e.stopPropagation()}>
      <h3>Assign Skills to {selectedAgent}</h3>
      <div class="skill-list">
        {#each skills as skill}
          <label class="skill-option">
            <input
              type="checkbox"
              checked={selectedSkillsForAssignment.includes(skill.name)}
              onchange={() => toggleSkillAssignment(skill.name)}
            />
            <span class="skill-option-name">{skill.name}</span>
            <span class="skill-option-desc">{skill.description}</span>
          </label>
        {/each}
        {#if skills.length === 0}
          <div class="empty-state">No skills available. Create some first.</div>
        {/if}
      </div>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (showSkillAssignment = false)}>Cancel</button>
        <button class="btn-primary" onclick={handleAssignSkills}>
          Assign {selectedSkillsForAssignment.length} skill{selectedSkillsForAssignment.length !== 1 ? 's' : ''}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Create Rule Modal -->
{#if showCreateRule}
  <div class="modal-overlay" onclick={() => (showCreateRule = false)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Create New Rule</h3>
      <div class="form-group">
        <label>Name (filename)</label>
        <input type="text" bind:value={newRuleName} placeholder="e.g. coding-standards" />
      </div>
      <div class="form-group">
        <label>Title</label>
        <input type="text" bind:value={newRuleTitle} placeholder="e.g. Coding Standards" />
      </div>
      <div class="form-group">
        <label>Body (markdown)</label>
        <textarea bind:value={newRuleBody} rows="10" placeholder="# Rule Title&#10;&#10;Rule content in markdown..."></textarea>
      </div>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (showCreateRule = false)}>Cancel</button>
        <button class="btn-primary" onclick={handleCreateRule} disabled={!newRuleName.trim()}>Create</button>
      </div>
    </div>
  </div>
{/if}

<!-- Edit Rule Modal -->
{#if editRule}
  <div class="modal-overlay" onclick={() => (editRule = null)} role="presentation">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <h3>Edit Rule: {editRule.name}</h3>
      <div class="form-group">
        <label>Title</label>
        <input type="text" bind:value={editRule.title} />
      </div>
      <div class="form-group">
        <label>Body (markdown)</label>
        <textarea bind:value={editRule.body} rows="14"></textarea>
      </div>
      <div class="modal-actions">
        <button class="btn-secondary" onclick={() => (editRule = null)}>Cancel</button>
        <button class="btn-primary" onclick={handleUpdateRule}>Save</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .agents-page {
    height: 100%;
    display: flex;
    flex-direction: column;
  }

  .loading, .error {
    text-align: center;
    padding: 48px;
    color: #8b949e;
  }
  .error { color: #f85149; }

  .error-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 14px;
    background: rgba(248,81,73,0.1);
    border: 1px solid #f85149;
    border-radius: 6px;
    color: #f85149;
    font-size: 13px;
    margin-bottom: 12px;
    flex-shrink: 0;
  }
  .error-banner .btn-sm {
    padding: 2px 8px;
    font-size: 11px;
    border-color: #f85149;
    color: #f85149;
    background: transparent;
    cursor: pointer;
    border: 1px solid #f85149;
    border-radius: 4px;
  }

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 16px;
    flex-shrink: 0;
  }

  .view-toggle {
    display: flex;
    gap: 2px;
    background: #161b22;
    border-radius: 8px;
    padding: 3px;
  }
  .view-toggle button {
    padding: 6px 14px;
    background: transparent;
    border: none;
    border-radius: 6px;
    color: #8b949e;
    cursor: pointer;
    font-size: 13px;
    transition: all 0.15s;
  }
  .view-toggle button.active {
    background: #21262d;
    color: #c9d1d9;
  }
  .view-toggle button:hover:not(.active) { color: #c9d1d9; }

  .format-filter {
    display: flex;
    gap: 2px;
    background: #161b22;
    border-radius: 8px;
    padding: 3px;
  }
  .format-filter button {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 5px 12px;
    background: transparent;
    border: none;
    border-radius: 6px;
    color: #8b949e;
    cursor: pointer;
    font-size: 12px;
    transition: all 0.15s;
  }
  .format-filter button.active {
    background: #21262d;
    color: #c9d1d9;
  }
  .format-filter button:hover:not(.active) { color: #c9d1d9; }

  .btn-primary {
    padding: 6px 14px;
    background: #238636;
    color: white;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    transition: background 0.15s;
  }
  .btn-primary:hover { background: #2ea043; }
  .btn-primary:disabled { opacity: 0.5; cursor: default; }

  .btn-secondary {
    padding: 6px 14px;
    background: #21262d;
    color: #c9d1d9;
    border: 1px solid #30363d;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
  }
  .btn-secondary:hover { background: #30363d; }

  .btn-sm {
    padding: 3px 8px;
    background: transparent;
    border: 1px solid #30363d;
    border-radius: 4px;
    color: #8b949e;
    cursor: pointer;
    font-size: 12px;
    transition: all 0.15s;
  }
  .btn-sm:hover { color: #c9d1d9; border-color: #8b949e; }
  .btn-danger:hover { color: #f85149; border-color: #f85149; }
  .btn-danger.confirming { color: #f85149; border-color: #f85149; background: rgba(248,81,73,0.1); }

  .agents-grid, .skills-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 12px;
    overflow-y: auto;
    flex: 1;
    padding-bottom: 20px;
  }

  .agent-card, .skill-card {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 8px;
    padding: 14px;
    transition: border-color 0.15s;
  }
  .agent-card:hover, .skill-card:hover { border-color: #484f58; }
  .agent-card.selected { border-color: #58a6ff; }

  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 8px;
  }
  .card-title {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .card-actions { display: flex; gap: 4px; flex-shrink: 0; }

  .agent-name, .skill-name {
    font-weight: 600;
    color: #58a6ff;
    font-size: 14px;
  }

  .format-badges { display: flex; gap: 4px; align-items: center; }

  .badge {
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 4px;
    font-weight: 600;
    text-transform: uppercase;
  }
  .badge.cc { background: #1f3a5f; color: #58a6ff; }
  .badge.oc { background: #3a2f1f; color: #d29922; }
  .badge.model { background: #1f3a1f; color: #3fb950; }
  .badge.model.oc-model { background: #1a2f3a; color: #58a6ff; }
  .badge.locked { background: #3a1f2f; }
  .badge.resources { background: #2f1f3a; }

  .card-description {
    color: #8b949e;
    font-size: 12px;
    margin-bottom: 8px;
    line-height: 1.4;
  }

  .card-tools {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-bottom: 8px;
  }
  .tool-tag {
    font-size: 11px;
    padding: 1px 6px;
    background: #21262d;
    border-radius: 3px;
    color: #7ee787;
  }
  .tool-tag.more { color: #8b949e; }

  .card-skills {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .card-skills.empty {
    color: #484f58;
    font-size: 11px;
    font-style: italic;
  }
  .skill-chip {
    font-size: 11px;
    padding: 2px 8px;
    background: #1f3a5f;
    border-radius: 10px;
    color: #79c0ff;
    cursor: pointer;
    transition: background 0.15s;
  }
  .skill-chip:hover { background: #264d7a; }

  .card-hint {
    font-size: 12px;
    color: #8b949e;
    margin-bottom: 6px;
  }
  .card-hint code {
    background: #21262d;
    padding: 1px 5px;
    border-radius: 3px;
    font-family: monospace;
    font-size: 11px;
  }

  .card-resources {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 6px;
  }
  .resource-tag {
    font-size: 11px;
    padding: 1px 6px;
    background: #2f1f3a;
    border-radius: 3px;
    color: #d2a8ff;
  }

  .skills-header {
    margin-bottom: 12px;
  }
  .search-input {
    width: 100%;
    padding: 8px 12px;
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 6px;
    color: #c9d1d9;
    font-size: 13px;
    outline: none;
    transition: border-color 0.15s;
  }
  .search-input:focus { border-color: #58a6ff; }

  /* Modals */
  .modal-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }
  .modal {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 12px;
    padding: 20px;
    width: 90%;
    max-width: 560px;
    max-height: 80vh;
    overflow-y: auto;
  }
  .modal.modal-wide { max-width: 720px; }
  .modal h3 {
    margin-bottom: 16px;
    color: #c9d1d9;
  }

  .form-group {
    margin-bottom: 12px;
  }
  .form-group label {
    display: block;
    font-size: 12px;
    color: #8b949e;
    margin-bottom: 4px;
  }
  .form-group input[type="text"],
  .form-group textarea,
  .form-group select {
    width: 100%;
    padding: 8px 10px;
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 6px;
    color: #c9d1d9;
    font-size: 13px;
    font-family: inherit;
    outline: none;
    resize: vertical;
  }
  .form-group input:focus,
  .form-group textarea:focus,
  .form-group select:focus {
    border-color: #58a6ff;
  }
  .form-group label:has(input[type="checkbox"]) {
    display: flex;
    align-items: center;
    gap: 6px;
    color: #c9d1d9;
    font-size: 13px;
  }
  .form-row {
    display: flex;
    gap: 12px;
  }
  .form-row .form-group { flex: 1; }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }

  .skill-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 400px;
    overflow-y: auto;
  }
  .skill-option {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 6px;
    cursor: pointer;
    transition: border-color 0.15s;
  }
  .skill-option:hover { border-color: #484f58; }
  .skill-option input[type="checkbox"] {
    flex-shrink: 0;
    accent-color: #58a6ff;
  }
  .skill-option-name {
    font-weight: 600;
    color: #79c0ff;
    font-size: 13px;
    min-width: 120px;
  }
  .skill-option-desc {
    color: #8b949e;
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }

  .empty-state {
    text-align: center;
    padding: 24px;
    color: #484f58;
    font-size: 13px;
  }

  .badge.rule-badge {
    background: #2f2f1f;
    color: #e3b341;
  }

  .rule-preview {
    white-space: pre-wrap;
    font-family: monospace;
    font-size: 11px;
    max-height: 80px;
    overflow: hidden;
  }

  /* Skill creation picker */
  .skill-create-options {
    display: flex;
    gap: 12px;
    margin-bottom: 4px;
  }
  .create-option {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 18px 12px;
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 8px;
    cursor: pointer;
    transition: border-color 0.15s, background 0.15s;
    color: inherit;
    text-align: center;
  }
  .create-option:hover {
    border-color: #58a6ff;
    background: #161b22;
  }
  .option-icon { font-size: 24px; }
  .option-label {
    font-weight: 600;
    color: #c9d1d9;
    font-size: 14px;
  }
  .option-desc {
    color: #8b949e;
    font-size: 12px;
    line-height: 1.4;
  }
  .option-desc code {
    background: #21262d;
    padding: 1px 4px;
    border-radius: 3px;
    font-size: 11px;
  }

  /* Skill creator launcher */
  .launcher-desc {
    color: #8b949e;
    font-size: 13px;
    margin-bottom: 14px;
    line-height: 1.5;
  }
  .launcher-desc code {
    background: #21262d;
    padding: 1px 4px;
    border-radius: 3px;
  }
  .target-toggle {
    display: flex;
    gap: 2px;
    background: #0d1117;
    border-radius: 8px;
    padding: 3px;
    margin-bottom: 12px;
  }
  .target-toggle button {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 7px 14px;
    background: transparent;
    border: none;
    border-radius: 6px;
    color: #8b949e;
    cursor: pointer;
    font-size: 13px;
    transition: all 0.15s;
  }
  .target-toggle button.active {
    background: #21262d;
    color: #c9d1d9;
  }
  .target-toggle button:hover:not(.active) { color: #c9d1d9; }
  .command-block {
    display: flex;
    align-items: center;
    gap: 10px;
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 6px;
    padding: 10px 14px;
    margin-bottom: 12px;
  }
  .command-text {
    flex: 1;
    font-family: monospace;
    font-size: 14px;
    color: #79c0ff;
    user-select: all;
  }
  .launcher-note {
    color: #8b949e;
    font-size: 12px;
    margin: 0;
  }
  .launcher-note strong { color: #c9d1d9; }

  .agent-format-note {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
    padding: 10px 12px;
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 6px;
  }
  .format-note-row {
    display: flex;
    align-items: baseline;
    gap: 8px;
    font-size: 12px;
    color: #8b949e;
  }
  .format-note-row code {
    background: #21262d;
    padding: 1px 4px;
    border-radius: 3px;
    font-size: 11px;
  }
</style>
