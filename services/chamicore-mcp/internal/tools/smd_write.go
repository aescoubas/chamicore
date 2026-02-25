package tools

import (
	"context"
	"strings"

	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

func (r *Runner) smdGroupsMembersAdd(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		Label   string   `json:"label"`
		Members []string `json:"members"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}

	label := strings.TrimSpace(req.Label)
	members := trimStringList(req.Members)
	if label == "" {
		return nil, validationErrorf("label is required")
	}
	if len(members) == 0 {
		return nil, validationErrorf("members must contain at least one component id")
	}

	if err := r.smd.AddGroupMembers(ctx, label, smdtypes.AddMembersRequest{
		Members: members,
	}); err != nil {
		return nil, mapExecutionError(err, "adding SMD group members")
	}

	group, err := r.smd.GetGroup(ctx, label)
	if err != nil {
		return nil, mapExecutionError(err, "loading SMD group after member add")
	}
	return toMap(group)
}

func (r *Runner) smdGroupsMembersRemove(ctx context.Context, args map[string]any) (map[string]any, error) {
	var req struct {
		Label   string   `json:"label"`
		Members []string `json:"members"`
	}
	if err := decodeArgsStrict(args, &req); err != nil {
		return nil, err
	}

	label := strings.TrimSpace(req.Label)
	members := trimStringList(req.Members)
	if label == "" {
		return nil, validationErrorf("label is required")
	}
	if len(members) == 0 {
		return nil, validationErrorf("members must contain at least one component id")
	}

	for _, member := range members {
		if err := r.smd.RemoveGroupMember(ctx, label, member); err != nil {
			return nil, mapExecutionError(err, "removing SMD group member")
		}
	}

	group, err := r.smd.GetGroup(ctx, label)
	if err != nil {
		return nil, mapExecutionError(err, "loading SMD group after member remove")
	}
	return toMap(group)
}
