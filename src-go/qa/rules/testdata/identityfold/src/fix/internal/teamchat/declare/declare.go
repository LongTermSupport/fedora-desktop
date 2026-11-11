// Package declare stands for the reader of a team's declaration: a forge section a person wrote,
// read by the strict canonical decoder (the originating defect), and read by the input decoder.
package declare

import "fix/internal/teamchat/proto"

// trust is the originating defect: the person's forge section reaches the lower-case grammar
// unfolded, so `org: ExampleOrg` is refused.
func trust(v proto.Value) (proto.Forge, error) {
	return proto.DecodeForge(v) // want `proto.DecodeForge given a name that is not folded first \(project_identity_fold\)`
}

// trustInput is the fix: the input decoder folds before it validates.
func trustInput(v proto.Value) (proto.Forge, error) {
	return proto.DecodeForgeInput(v)
}

// owner checks a name a flag gave against the owner grammar, unfolded, and then folded.
func owner(flag string) bool {
	if proto.ValidOwner(flag) { // want `proto.ValidOwner given a name that is not folded first`
		return true
	}
	return proto.ValidOwner(proto.LowercaseRefRepo(flag))
}

// repo is the same for an OWNER/REPO, as a function value too.
func repo(flag string) bool {
	check := proto.ValidOwnerRepo // want `proto.ValidOwnerRepo given a name that is not folded first`
	return check(flag)
}

// forge builds a section by hand from a name a person gave.
func forge(org, name string, excluded []string) proto.Forge {
	f := proto.Forge{Kind: "github", Org: org} // want `a name written to proto.Forge.Org that is not folded first`
	f.Exclude = excluded                         // want `a name written to proto.Forge.Exclude that is not folded first`
	f.Repos = append(f.Repos, proto.ForgeRepo{Repo: name}) // want `a name written to proto.ForgeRepo.Repo that is not folded first`
	f.Org = ""
	return f
}
