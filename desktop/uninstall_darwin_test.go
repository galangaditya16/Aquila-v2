package main

import "testing"

func TestUninstallOnlyTargetsAppBundle(t *testing.T) {
	for _, test := range []struct {
		executable string
		want       string
	}{
		{"/Applications/Aquila.app/Contents/MacOS/aquila", "/Applications/Aquila.app"},
		{"/Users/test/My Apps/aquila.app/Contents/MacOS/aquila", "/Users/test/My Apps/aquila.app"},
		{"/Users/test/project/desktop/bin/aquila", ""},
		{"/Applications/Other/Contents/MacOS/aquila", ""},
		{"/Volumes/Aquila/aquila.app/Contents/MacOS/aquila", ""},
	} {
		plan, err := prepareUninstall(test.executable)
		if test.want == "" {
			if err == nil || plan.run != nil {
				t.Errorf("uninstall allowed for %s", test.executable)
			}
			continue
		}
		bundle, err := macAppBundle(test.executable)
		if err != nil || bundle != test.want || plan.run == nil {
			t.Errorf("bundle for %s = %q, error = %v", test.executable, bundle, err)
		}
	}
}
