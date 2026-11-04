package api

// Serialize evidence reconciliation with phase gates. The manager notification
// may be coalesced; the guard also reconciles before the lead changes a workflow.
func (s *Server) syncManagedTeamState() error {
	if s.teams == nil || s.db == nil {
		return nil
	}
	s.teamEvidenceMu.Lock()
	defer s.teamEvidenceMu.Unlock()
	project, _, err := s.teamProject()
	if err != nil {
		return err
	}
	list, err := s.teams.List(project)
	if err != nil {
		return err
	}
	for _, team := range list {
		for _, member := range team.Members {
			if member.NativeID == "" {
				continue
			}
			key := teamSessionKey(member)
			if peer, e := s.db.GetPeerBySession(key); e != nil || peer.ProjectID != project {
				if _, e = s.db.UpsertPeerInProject(key, member.Profile.Runtime, "", team.ProjectRoot); e != nil {
					return e
				}
			}
			if team.State == "stopped" {
				if err := s.db.EndPeer(key); err != nil {
					return err
				}
			}
		}
		if s.coordinator != nil {
			if err := s.syncTeamWorkflowEvidence(team); err != nil {
				return err
			}
		}
	}
	return nil
}
