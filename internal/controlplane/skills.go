package controlplane

import (
	"context"
	"sort"

	"agenthub/pkg/protocol"
)

func (s *Server) resolveSessionSkills(ctx context.Context, workspaceID string) ([]protocol.ResolvedSkill, error) {
	candidates, err := s.store.ListSkillCandidates(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	resolvedBySlug := map[string]SkillDefinitionWithFiles{}
	shadowedBySlug := map[string][]protocol.SkillShadow{}
	for _, candidate := range candidates {
		if candidate.Definition.Slug == "" || len(candidate.Files) == 0 {
			continue
		}
		current, exists := resolvedBySlug[candidate.Definition.Slug]
		if !exists || shouldReplaceSkill(candidate.Definition, current.Definition) {
			if exists {
				shadowedBySlug[candidate.Definition.Slug] = append(shadowedBySlug[candidate.Definition.Slug], skillShadow(current.Definition))
			}
			resolvedBySlug[candidate.Definition.Slug] = candidate
			continue
		}
		shadowedBySlug[candidate.Definition.Slug] = append(shadowedBySlug[candidate.Definition.Slug], skillShadow(candidate.Definition))
	}

	slugs := make([]string, 0, len(resolvedBySlug))
	for slug := range resolvedBySlug {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	resolved := make([]protocol.ResolvedSkill, 0, len(slugs))
	for _, slug := range slugs {
		skill := resolvedBySlug[slug]
		files := make([]protocol.ResolvedSkillFile, 0, len(skill.Files))
		for _, file := range skill.Files {
			files = append(files, protocol.ResolvedSkillFile{Path: file.Path, Content: file.Content, ContentHash: file.ContentHash})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		shadowed := shadowedBySlug[slug]
		sort.Slice(shadowed, func(i, j int) bool {
			leftPriority := skillSourcePriority(shadowed[i].Source)
			rightPriority := skillSourcePriority(shadowed[j].Source)
			if leftPriority == rightPriority {
				return shadowed[i].Version > shadowed[j].Version
			}
			return leftPriority > rightPriority
		})
		resolved = append(resolved, protocol.ResolvedSkill{
			Slug:         skill.Definition.Slug,
			Source:       skill.Definition.Source,
			DefinitionID: skill.Definition.ID,
			Version:      skill.Definition.Version,
			ContentHash:  skill.Definition.ContentHash,
			Files:        files,
			Shadowed:     shadowed,
		})
	}
	return resolved, nil
}

func shouldReplaceSkill(candidate, current SkillDefinition) bool {
	candidatePriority := skillSourcePriority(candidate.Source)
	currentPriority := skillSourcePriority(current.Source)
	if candidatePriority != currentPriority {
		return candidatePriority > currentPriority
	}
	if candidate.Version != current.Version {
		return candidate.Version > current.Version
	}
	return candidate.UpdatedAt.After(current.UpdatedAt)
}

func skillSourcePriority(source string) int {
	switch source {
	case protocol.SkillSourceUser:
		return 4
	case protocol.SkillSourcePlugin:
		return 3
	case protocol.SkillSourceWorkspace:
		return 2
	case protocol.SkillSourcePlatformBuiltin:
		return 1
	default:
		return 0
	}
}

func skillShadow(def SkillDefinition) protocol.SkillShadow {
	return protocol.SkillShadow{Source: def.Source, DefinitionID: def.ID, Version: def.Version, ContentHash: def.ContentHash}
}
