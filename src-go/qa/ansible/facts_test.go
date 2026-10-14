package ansible

import "testing"

func TestSelfDefaultMatch(t *testing.T) {
	cases := map[string]bool{
		`foo: "{{ foo | default(1) }}"`:               true,
		`foo: '{{ foo | default(1) }}'`:               true,
		`foo: {{ foo | default(1) }}`:                 true,
		`foo: "{{foo|default(1)}}"`:                   true,
		`    foo_bar2: "{{ foo_bar2 | default(1) }}"`: true,
		`foo:"{{ foo | default(1) }}"`:                true,
		`foo: "{{ foo | default`:                      true,
		`foo: "{{ bar | default(1) }}"`:               false,
		`foo: "{{ foo }}"`:                            false,
		`foo: "{{ foobar | default(1) }}"`:            false,
		`foobar: "{{ foo | default(1) }}"`:            false,
		`foo: "{{ foo | defaults }}"`:                 false,
		`foo: "{{ foo | default_x }}"`:                false,
		`# foo: "{{ foo | default(1) }}"`:             false,
		`- foo: "{{ foo | default(1) }}"`:             false,
		`foo: "{{ Foo | default(1) }}"`:               false,
		`foo: "{{ foo | DEFAULT(1) }}"`:               false,
		`1foo: "{{ 1foo | default(1) }}"`:             false,
		`x: 1 foo: "{{ foo | default(1) }}"`:          false,
	}
	for text, want := range cases {
		if got := selfDefaultMatch(text); got != want {
			t.Errorf("selfDefaultMatch(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestFactVarMatch(t *testing.T) {
	cases := map[string]bool{
		"x: ansible_env":                                true,
		"x: ansible_distribution_major_version":         true,
		"x: ansible_facts['env']":                       false,
		"x: ansible_version":                            false,
		"x: ansible_envx":                               false,
		"x: my_ansible_env":                             false,
		"x: ansible_env_x":                              false,
		"x: Ansible_Env":                                false,
		"x: ansible_env ansible_fqdn":                   true,
		"u: getent_passwd[user]":                        true,
		"u: ansible_facts['getent_passwd'][u]":          false,
		`u: ansible_facts["getent_passwd"][u]`:          false,
		"u: ansible_facts.getent_passwd[u]":             false,
		"u: ansible_facts['getent_group'] getent_group": true,
		"u: getent_group ansible_facts['getent_group']": true,
		"u: getent_passwd_x":                            false,
		"u: xgetent_passwd":                             false,
		"u: getent_other":                               false,
		"u: ansible_getent_passwd":                      false,
		"u: ansible_facts.getent_passwd.root":           false,
		"u: ansible_facts [ 'getent_passwd' ]":          true,
		"u: ansible_facts['x']['getent_passwd']":        true,
		"u: getent_ahostsv6":                            true,
	}
	for text, want := range cases {
		if got := factVarMatch(text); got != want {
			t.Errorf("factVarMatch(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestRuntimeDirMatch(t *testing.T) {
	cases := map[string]bool{
		"path: /run/user/1000/bus": true,
		"path: /run/user/0":        true,
		`path: "/run/user/{{ user_login_uid | default(1000) }}/bus"`:     true,
		"path: /run/user/{{ uid }}":                                      true,
		"path: getent_passwd /run/user/{{ uid }}":                        true,
		"path: /run/user/{{ a }} /run/user/{{ getent_passwd }}":          false,
		"path: /run/user/{{ getent_passwd }} /run/user/9":                true,
		"path: /run/user/{{ ansible_facts['getent_passwd'][u][1] }}/bus": false,
		"path: /run/user/":                         false,
		"path: /run/user/$UID":                     false,
		"path: /run/user/{ uid }":                  false,
		"path: /run/users/1000":                    false,
		"path: /run/user/{{ uid }} #getent_passwd": false,
	}
	for text, want := range cases {
		if got := runtimeDirMatch(text); got != want {
			t.Errorf("runtimeDirMatch(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestCommentOnly(t *testing.T) {
	cases := map[string]bool{
		"# c": true, "   # c": true, "\t# c": true, "x # c": false, "": false, "  ": false,
	}
	for text, want := range cases {
		if got := commentOnly(text); got != want {
			t.Errorf("commentOnly(%q) = %v, want %v", text, got, want)
		}
	}
}
