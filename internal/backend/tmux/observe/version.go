package observe

import "fmt"

func headerLayoutForVersion(version string) (headerLayout, error) {
	switch version {
	case "3.2", "3.2a", "3.3", "3.3a", "3.4", "3.5", "3.5a":
		return headerLegacy, nil
	case "3.6", "3.6a", "3.6b", "3.7", "3.7a", "3.7b", "3.7c", "3.8":
		return headerCurrent, nil
	default:
		return 0, fmt.Errorf("unsupported native tmux version %q; supported stable releases are 3.2 through 3.8", version)
	}
}
