package update

import (
	"fmt"
	"strings"
)

// Version is a strict `vX.Y.Z[-prerelease][+build]` semver tag (D4).
type Version struct {
	Major, Minor, Patch string
	Pre                 string // without the leading '-'
	Build               string // without the leading '+'
}

// ParseVersion parses a strict semver tag. It is deliberately stricter than
// the Go module proxy: missing minor/patch, signs, and padded numeric
// identifiers are rejected so unparsable tags can never count as "newer".
func ParseVersion(tag string) (Version, error) {
	var v Version
	body, ok := strings.CutPrefix(tag, "v")
	if !ok || body == "" {
		return Version{}, fmt.Errorf("version %q must start with 'v'", tag)
	}
	if i := strings.IndexByte(body, '+'); i >= 0 {
		v.Build = body[i+1:]
		body = body[:i]
		if err := validateIdentifiers(v.Build, true); err != nil {
			return Version{}, fmt.Errorf("version %q: invalid build metadata: %w", tag, err)
		}
	}
	if i := strings.IndexByte(body, '-'); i >= 0 {
		v.Pre = body[i+1:]
		body = body[:i]
		if err := validateIdentifiers(v.Pre, false); err != nil {
			return Version{}, fmt.Errorf("version %q: invalid prerelease: %w", tag, err)
		}
	}
	parts := strings.Split(body, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q must be vX.Y.Z", tag)
	}
	for _, p := range parts {
		if err := checkNumeric(p); err != nil {
			return Version{}, fmt.Errorf("version %q: %w", tag, err)
		}
	}
	v.Major, v.Minor, v.Patch = parts[0], parts[1], parts[2]
	return v, nil
}

func checkNumeric(p string) error {
	if p == "" {
		return fmt.Errorf("empty numeric component")
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return fmt.Errorf("numeric component %q contains non-digits", p)
		}
	}
	if len(p) > 1 && p[0] == '0' {
		return fmt.Errorf("numeric component %q has a leading zero", p)
	}
	return nil
}

func validateIdentifiers(s string, build bool) error {
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return fmt.Errorf("empty identifier")
		}
		for _, r := range id {
			if !isSemverIdentRune(r) {
				return fmt.Errorf("identifier %q contains invalid characters", id)
			}
		}
		// Numeric prerelease identifiers must not be zero-padded; build
		// metadata identifiers may be.
		if !build && len(id) > 1 && id[0] == '0' && numericIdentifier(id) {
			return fmt.Errorf("numeric identifier %q has a leading zero", id)
		}
	}
	return nil
}

func isSemverIdentRune(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-'
}

func (v Version) IsPrerelease() bool { return v.Pre != "" }

// Compare orders Versions per semver: prerelease < release, numeric
// prerelease identifiers compare numerically and sort before alphanumeric
// ones, and build metadata is ignored.
func (v Version) Compare(o Version) int {
	for _, pair := range [][2]string{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if pair[0] != pair[1] {
			return cmpNumeric(pair[0], pair[1])
		}
	}
	switch {
	case v.Pre == "" && o.Pre == "":
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	}
	a := strings.Split(v.Pre, ".")
	b := strings.Split(o.Pre, ".")
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		aNumeric, bNumeric := numericIdentifier(a[i]), numericIdentifier(b[i])
		switch {
		case aNumeric && bNumeric:
			return cmpNumeric(a[i], b[i])
		case aNumeric:
			return -1
		case bNumeric:
			return 1
		default:
			return strings.Compare(a[i], b[i])
		}
	}
	return cmpInt(len(a), len(b))
}

func numericIdentifier(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func cmpNumeric(a, b string) int {
	if len(a) != len(b) {
		return cmpInt(len(a), len(b))
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func (v Version) String() string {
	s := fmt.Sprintf("v%s.%s.%s", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// decideUpdate reports whether hetki should install targetTag over
// currentTag (D4 policy). exact selects the target explicitly and permits
// any direction including reinstalls. Release targets must be stable;
// the default path only moves strictly forward. Unparsable versions fail closed.
func decideUpdate(currentTag, targetTag string, exact bool) (install bool, reason string, err error) {
	target, err := ParseVersion(targetTag)
	if err != nil {
		return false, "", fmt.Errorf("target version rejected: %w", err)
	}
	if target.IsPrerelease() {
		return false, "", fmt.Errorf("target %s is a prerelease; select a stable release or use --head for main", targetTag)
	}
	if exact {
		return true, "explicit version selection", nil
	}
	if currentTag == "" {
		return false, "", fmt.Errorf("cannot determine current version; pass --version to select one explicitly")
	}
	if currentTag == "dev" {
		return true, "development build", nil
	}
	current, err := ParseVersion(currentTag)
	if err != nil {
		return false, "", fmt.Errorf("current version %q is not a release tag; pass --version to select one explicitly", currentTag)
	}
	switch current.Compare(target) {
	case 0:
		return false, "already on the latest version", nil
	case 1:
		return false, fmt.Sprintf("current %s is newer than latest published %s", current, target), nil
	default:
		return true, "newer version available", nil
	}
}
