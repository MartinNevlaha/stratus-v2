package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

func scanWikiPage(row *sql.Row) (*WikiPage, error) {
	var p WikiPage
	var hashesJSON, tagsJSON, metaJSON string

	err := row.Scan(
		&p.ID, &p.PageType, &p.Title, &p.Content, &p.Status, &p.StalenessScore,
		&hashesJSON, &tagsJSON, &metaJSON,
		&p.GeneratedBy, &p.Version, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan wiki page: %w", err)
	}

	if err := json.Unmarshal([]byte(hashesJSON), &p.SourceHashes); err != nil {
		log.Printf("warning: failed to parse source_hashes for wiki page %s: %v", p.ID, err)
	}
	if err := json.Unmarshal([]byte(tagsJSON), &p.Tags); err != nil {
		log.Printf("warning: failed to parse tags for wiki page %s: %v", p.ID, err)
	}
	if err := json.Unmarshal([]byte(metaJSON), &p.Metadata); err != nil {
		log.Printf("warning: failed to parse metadata for wiki page %s: %v", p.ID, err)
	}

	if p.SourceHashes == nil {
		p.SourceHashes = []string{}
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	return &p, nil
}

func scanWikiPages(rows *sql.Rows) ([]WikiPage, error) {
	var pages []WikiPage
	for rows.Next() {
		var p WikiPage
		var hashesJSON, tagsJSON, metaJSON string

		if err := rows.Scan(
			&p.ID, &p.PageType, &p.Title, &p.Content, &p.Status, &p.StalenessScore,
			&hashesJSON, &tagsJSON, &metaJSON,
			&p.GeneratedBy, &p.Version, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan wiki page: %w", err)
		}

		if err := json.Unmarshal([]byte(hashesJSON), &p.SourceHashes); err != nil {
			log.Printf("warning: failed to parse source_hashes for wiki page %s: %v", p.ID, err)
		}
		if err := json.Unmarshal([]byte(tagsJSON), &p.Tags); err != nil {
			log.Printf("warning: failed to parse tags for wiki page %s: %v", p.ID, err)
		}
		if err := json.Unmarshal([]byte(metaJSON), &p.Metadata); err != nil {
			log.Printf("warning: failed to parse metadata for wiki page %s: %v", p.ID, err)
		}

		if p.SourceHashes == nil {
			p.SourceHashes = []string{}
		}
		if p.Tags == nil {
			p.Tags = []string{}
		}
		if p.Metadata == nil {
			p.Metadata = map[string]any{}
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

func scanWikiLinks(rows *sql.Rows) ([]WikiLink, error) {
	var links []WikiLink
	for rows.Next() {
		var l WikiLink
		if err := rows.Scan(
			&l.ID, &l.FromPageID, &l.ToPageID, &l.LinkType, &l.Strength, &l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan wiki link: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

func scanWikiPageRefs(rows *sql.Rows) ([]WikiPageRef, error) {
	var refs []WikiPageRef
	for rows.Next() {
		var r WikiPageRef
		if err := rows.Scan(
			&r.ID, &r.PageID, &r.SourceType, &r.SourceID, &r.Excerpt, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan wiki page ref: %w", err)
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}
