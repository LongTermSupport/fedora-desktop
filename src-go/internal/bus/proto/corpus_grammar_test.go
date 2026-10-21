package proto

import (
	"errors"
	"maps"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

var errNotAReference = errors.New("not a reference")

func handleJSON(h Handle, ok bool) any {
	if !ok {
		return nil
	}
	var n any
	if v, numbered := h.N(); numbered {
		n = v
	}
	return map[string]any{"repo": h.Repo, "seat": h.Seat, "host": h.Host, "type": h.Type, "n": n}
}

func refJSON(r Ref, ok bool) any {
	if !ok {
		return nil
	}
	out := map[string]any{
		"text": r.Text, "form": r.Form, "owner": r.Owner, "repo": r.Repo, "sha": nil, "num": nil, "path": nil,
	}
	if r.SHA != "" {
		out["sha"] = r.SHA
	}
	if r.Num != 0 {
		out["num"] = r.Num
	}
	if r.Path != "" {
		out["path"] = r.Path
	}
	return out
}

// onString is a predicate over strings that a non-string value never satisfies.
func onString(pred func(string) bool) adapter {
	return func(a *args) (any, error) {
		s, ok := a.value("value").(string)
		return ok && pred(s), nil
	}
}

func optional(s string, ok bool) any {
	if !ok {
		return nil
	}
	return s
}

func buildHandleAdapter(a *args) (any, error) {
	switch seat := a.value("seat").(type) {
	case pyjson.Number:
		n, fits := seat.Int64()
		if seat.Kind != pyjson.Int || !fits {
			return nil, errNotAReference
		}
		return BuildNumberedHandle(a.str("repo"), n, a.str("host"), a.str("type"))
	case string:
		return BuildHandle(a.str("repo"), seat, a.str("host"), a.str("type"))
	}
	return nil, ErrSeat
}

func verbRuleAdapter(a *args) (any, error) {
	rule, ok := Verbs[a.str("verb")]
	if !ok {
		return nil, ErrUnknownVerb
	}
	return map[string]any{
		"ref": string(rule.Ref), "forms": sortedCopy(rule.Forms), "re": string(rule.Re), "ack": rule.Ack,
		"senders": sortedCopy(rule.Senders), "external": rule.External, "to_humans": ToHumans(a.str("verb")),
	}, nil
}

func grammarAdapters() map[string]adapter {
	all := identifierAdapters()
	maps.Copy(all, buildRoleAdapters())
	return all
}

func identifierAdapters() map[string]adapter {
	return map[string]adapter{
		"content_size": func(a *args) (any, error) { return ContentSize(a.value("content")) },
		"is_team_name": onString(IsTeamName),
		"parse_handle": func(a *args) (any, error) {
			s, ok := a.value("value").(string)
			if !ok {
				return nil, nil
			}
			return handleJSON(ParseHandle(s)), nil
		},
		"is_seat_name":       onString(IsSeat),
		"is_human_localpart": onString(IsHumanLocalpart),
		"parse_user_id": func(a *args) (any, error) {
			s, ok := a.value("value").(string)
			if !ok {
				return nil, nil
			}
			return optional(ParseUserID(s, a.str("server_name"))), nil
		},
		"is_agent_user_id": func(a *args) (any, error) {
			return IsAgentUserID(a.value("value"), a.str("server_name")), nil
		},
		"is_human_user_id": func(a *args) (any, error) {
			return IsHumanUserID(a.value("value"), a.str("server_name")), nil
		},
		"is_room_id":     onString(IsRoomID),
		"is_event_id":    onString(IsEventID),
		"is_branch_name": onString(IsBranch),
		"is_owner":       onString(IsOwner),
		"is_repo_name":   onString(IsRepoName),
		"is_owner_repo":  onString(IsOwnerRepo),
		"parse_ref": func(a *args) (any, error) {
			s, ok := a.value("value").(string)
			if !ok {
				return nil, nil
			}
			return refJSON(ParseRef(s)), nil
		},
		"lowercase_ref_repo": func(a *args) (any, error) { return LowercaseRefRepo(a.str("value")), nil },
		"is_path_prefix":     onString(IsPathPrefix),
		"path_allowed": func(a *args) (any, error) {
			return PathAllowed(a.str("path"), a.strs("prefixes")), nil
		},
		"is_description": onString(IsDescription),
		"ref_trust": func(a *args) (any, error) {
			ref, ok := ParseRef(a.str("ref"))
			if !ok {
				return nil, errNotAReference
			}
			ctx := a.context()
			return map[string]any{
				"source": string(RefSource(ref, ctx)), "any_branch": TrustsAnyBranch(ref, ctx),
				"excluded": IsExcluded(ref, ctx),
			}, nil
		},
	}
}

func buildRoleAdapters() map[string]adapter {
	return map[string]adapter{
		"normalise_repo": func(a *args) (any, error) { return NormaliseRepo(a.str("raw")) },
		"repo_from_remote": func(a *args) (any, error) {
			remote, given := a.optStr("remote")
			if !given {
				remote = ""
			}
			return RepoFromRemote(remote, a.str("dir"))
		},
		"resolve_host": func(a *args) (any, error) {
			explicit, given := a.optStr("explicit")
			if !given {
				explicit = ""
			}
			role, hasRole := a.optStr("role")
			if !hasRole {
				role = ""
			}
			return ResolveHost(explicit, role)
		},
		"build_handle": buildHandleAdapter,
		"prefix_of":    func(a *args) (any, error) { return PrefixOf(a.str("handle")) },
		"normalise_role": func(a *args) (any, error) {
			if s, ok := a.value("value").(string); ok {
				return NormaliseRole(s), nil
			}
			return a.value("value"), nil
		},
		"verb_rule":    verbRuleAdapter,
		"coordinators": func(a *args) (any, error) { return Coordinators(a.context()), nil },
		"with_coordinator_copy": func(a *args) (any, error) {
			return WithCoordinatorCopy(a.strs("to"), a.str("sender"), a.context()), nil
		},
		"copy_of": func(a *args) (any, error) { return CopyOf(a.strs("to"), a.str("self"), a.context()), nil },
		"sender_class": func(a *args) (any, error) {
			if class := SenderClass(a.value("user"), a.context()); class != "" {
				return class, nil
			}
			return nil, nil
		},
		"addressable": func(a *args) (any, error) {
			verb, given := a.optStr("verb")
			if !given {
				verb = ""
			}
			return Addressable(a.str("user"), verb, a.context()), nil
		},
		"check_role": func(a *args) (any, error) {
			return nil, CheckRole(a.str("verb"), a.str("sender"), a.context())
		},
		"check_text_sender": func(a *args) (any, error) {
			return nil, CheckTextSender(a.str("sender"), a.context())
		},
	}
}
