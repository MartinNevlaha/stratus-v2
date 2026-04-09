<!-- Source: https://github.com/kepano/obsidian-skills (MIT License) -->
# Obsidian Flavored Markdown Skill

Create and edit valid Obsidian Flavored Markdown. Obsidian extends CommonMark and GFM with wikilinks, embeds, callouts, properties, comments, and other syntax.

## Internal Links (Wikilinks)

```
[[Note Name]]                          Link to note
[[Note Name|Display Text]]             Custom display text
[[Note Name#Heading]]                  Link to heading
[[#Heading in same note]]              Same-note heading link
```

## Embeds

Prefix any wikilink with `!` to embed its content inline:

```
![[Note Name]]                         Embed full note
![[image.png]]                         Embed image
![[image.png|300]]                     Embed image with width
```

## Callouts

```
> [!note]
> Basic callout.

> [!warning] Custom Title
> Callout with a custom title.

> [!faq]- Collapsed by default
> Foldable callout (- collapsed, + expanded).
```

Common types: note, tip, warning, info, example, quote, bug, danger, success, failure, question, abstract, todo.

## Properties (Frontmatter)

```yaml
---
title: My Note
date: 2024-01-15
tags:
  - project
  - active
aliases:
  - Alternative Name
---
```

## Tags

```
#tag                    Inline tag
#nested/tag             Nested tag with hierarchy
```

## Comments

```
This is visible %%but this is hidden%% text.
```

## Formatting

```
==Highlighted text==                   Highlight syntax
```
