package gktest

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

const (
	templateAlwaysValidate = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: alwaysvalidate
spec:
  crd:
    spec:
      names:
        kind: AlwaysValidate
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8salwaysvalidate
        violation[{"msg": msg}] {
          false
          msg := "should always pass"
        }
`

	templateNeverValidate = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: nevervalidate
spec:
  crd:
    spec:
      names:
        kind: NeverValidate
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8snevervalidate
        violation[{"msg": msg}] {
          true
          msg := "never validate"
        }
`

	templateNeverValidateTwice = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: nevervalidatetwice
spec:
  crd:
    spec:
      names:
        kind: NeverValidateTwice
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8snevervalidate
        violation[{"msg": msg}] {
          true
          msg := "first message"
        }

        violation[{"msg": msg}] {
          true
          msg := "second message"
        }
`

	templateUnsupportedVersion = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta2
metadata:
  name: unsupportedversion
spec:
  crd:
    spec:
      names:
        kind: UnsupportedVersion
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation[{"msg": msg}] {
          true
          msg := "never validate"
        }
`

	templateInvalidYAML = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: alwaysvalidate
  {}: {}
spec:
  crd:
    spec:
      names:
        kind: AlwaysValidate
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation[{"msg": msg}] {
          true
          msg := "never validate"
        }
`

	templateMarshalError = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: alwaysvalidate
spec: [a, b, c]
`

	templateCompileError = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: compileerror
spec:
  crd:
    spec:
      names:
        kind: CompileError
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation[{"msg": msg}] {
          f
          msg := "never validate"
        }
`

	constraintAlwaysValidate = `
kind: AlwaysValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-pass
`

	constraintNeverValidate = `
kind: NeverValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-fail
`

	constraintNeverValidateTwice = `
kind: NeverValidateTwice
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-fail-twice
`

	constraintInvalidYAML = `
kind: AlwaysValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-pass
  {}: {}
`

	constraintWrongTemplate = `
kind: Other
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: other
`

	object = `
kind: Object
apiVersion: v1
metadata:
  name: object`
)

func TestRunner_Run(t *testing.T) {
	testCases := []struct {
		name  string
		suite Suite
		f     fs.FS
		want  SuiteResult
	}{
		{
			name: "Suite missing Template",
			suite: Suite{
				Tests: []Test{{}},
			},
			f: fstest.MapFS{},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrInvalidSuite,
				}},
			},
		},
		{
			name: "Suite with template in nonexistent file",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: fs.ErrNotExist,
				}},
			},
		},
		{
			name: "Suite with YAML parsing error",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateInvalidYAML),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrAddingTemplate,
				}},
			},
		},
		{
			name: "Suite with template unmarshalling error",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateMarshalError),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrAddingTemplate,
				}},
			},
		},
		{
			name: "Suite with rego compilation error",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateCompileError),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrAddingTemplate,
				}},
			},
		},
		{
			name: "Suite with unsupported template version",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateUnsupportedVersion),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrAddingTemplate,
				}},
			},
		},
		{
			name: "Suite pointing to non-template",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(constraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrNotATemplate,
				}},
			},
		},
		{
			name: "Suite missing Constraint",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrInvalidSuite,
				}},
			},
		},
		{
			name: "valid Suite",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []Case{{
						Object: "object.yaml",
					}},
				}, {
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []Case{{
						Object: "object.yaml",
						Assertions: []Assertion{{
							Violations: intStrFromStr("yes"),
						}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintAlwaysValidate),
				},
				"deny-template.yaml": &fstest.MapFile{
					Data: []byte(templateNeverValidate),
				},
				"deny-constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintNeverValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(object),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{}},
				}, {
					CaseResults: []CaseResult{{}},
				}},
			},
		},
		{
			name: "valid Suite no cases",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{}},
			},
		},
		{
			name: "constraint missing file",
			suite: Suite{
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: fs.ErrNotExist,
				}},
			},
		},
		{
			name: "constraint invalid YAML",
			suite: Suite{
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintInvalidYAML),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrAddingConstraint,
				}},
			},
		},
		{
			name: "constraint is not a constraint",
			suite: Suite{
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrNotAConstraint,
				}},
			},
		},
		{
			name: "constraint is for other template",
			suite: Suite{
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintWrongTemplate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: ErrAddingConstraint,
				}},
			},
		},
		{
			name: "allow case missing file",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []Case{{
						Object: "object.yaml",
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Error: fs.ErrNotExist,
					}},
				}},
			},
		},
		{
			name: "deny case missing file",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []Case{{
						Object: "object.yaml",
						Assertions: []Assertion{{
							Violations: intStrFromStr("yes"),
						}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Error: fs.ErrNotExist,
					}},
				}},
			},
		},
		{
			name: "case without Object",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases:      []Case{{}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{Error: ErrInvalidCase}},
				}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			runner := Runner{
				FS:        tc.f,
				NewClient: NewOPAClient,
			}

			got := runner.Run(ctx, Filter{}, "", &tc.suite)

			if diff := cmp.Diff(tc.want, got, cmpopts.EquateErrors(), cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(SuiteResult{}, "Runtime"), cmpopts.IgnoreFields(TestResult{}, "Runtime"), cmpopts.IgnoreFields(CaseResult{}, "Runtime"),
			); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}

func TestRunner_Run_ClientError(t *testing.T) {
	want := SuiteResult{
		TestResults: []TestResult{{Error: ErrCreatingClient}},
	}

	runner := Runner{
		FS: fstest.MapFS{},
		NewClient: func() (Client, error) {
			return nil, errors.New("error")
		},
	}

	ctx := context.Background()

	suite := &Suite{
		Tests: []Test{{}},
	}
	got := runner.Run(ctx, Filter{}, "", suite)

	if diff := cmp.Diff(want, got, cmpopts.EquateErrors(), cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(SuiteResult{}, "Runtime"), cmpopts.IgnoreFields(TestResult{}, "Runtime"), cmpopts.IgnoreFields(CaseResult{}, "Runtime"),
	); diff != "" {
		t.Error(diff)
	}
}

func TestRunner_RunCase(t *testing.T) {
	testCases := []struct {
		name       string
		template   string
		constraint string
		object     string
		assertions []Assertion
		want       CaseResult
	}{
		// Validation successful
		{
			name:       "implicit expect allow",
			template:   templateAlwaysValidate,
			constraint: constraintAlwaysValidate,
			object:     object,
			assertions: nil,
			want:       CaseResult{},
		},
		{
			name:       "explicit expect allow boolean",
			template:   templateAlwaysValidate,
			constraint: constraintAlwaysValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromStr("no"),
			}},
			want: CaseResult{},
		},
		{
			name:       "implicit expect deny fail",
			template:   templateAlwaysValidate,
			constraint: constraintAlwaysValidate,
			object:     object,
			assertions: []Assertion{{}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "explicit expect deny boolean fail",
			template:   templateAlwaysValidate,
			constraint: constraintAlwaysValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromStr("yes"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect allow int",
			template:   templateAlwaysValidate,
			constraint: constraintAlwaysValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromInt(0),
			}},
			want: CaseResult{},
		},
		{
			name:       "expect deny int fail",
			template:   templateAlwaysValidate,
			constraint: constraintAlwaysValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromInt(1),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect deny message fail",
			template:   templateAlwaysValidate,
			constraint: constraintAlwaysValidate,
			object:     object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("first message"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		// Single violation
		{
			name:       "implicit expect deny",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{}},
			want:       CaseResult{},
		},
		{
			name:       "expect deny bool",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromStr("yes"),
			}},
			want: CaseResult{},
		},
		{
			name:       "expect deny int",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromInt(1),
			}},
			want: CaseResult{},
		},
		{
			name:       "expect deny int not enough violations",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromInt(2),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect allow bool fail",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromStr("no"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect allow int fail",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromInt(0),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect deny message",
			template:   templateAlwaysValidate,
			constraint: constraintAlwaysValidate,
			object:     object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("never validate"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "message valid regex",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("[enrv]+ [adeiltv]+"),
			}},
			want: CaseResult{},
		},
		{
			name:       "message invalid regex",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("never validate [("),
			}},
			want: CaseResult{
				Error: ErrInvalidRegex,
			},
		},
		{
			name:       "message missing regex",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("[enrv]+x [adeiltv]+"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		// Deny multiple violations
		{
			name:       "multiple violations count",
			template:   templateNeverValidateTwice,
			constraint: constraintNeverValidateTwice,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromInt(2),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation both messages implicit count",
			template:   templateNeverValidateTwice,
			constraint: constraintNeverValidateTwice,
			object:     object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("first message"),
			}, {
				Message: pointer.StringPtr("second message"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation both messages explicit count",
			template:   templateNeverValidateTwice,
			constraint: constraintNeverValidateTwice,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromInt(1),
				Message:    pointer.StringPtr("first message"),
			}, {
				Violations: intStrFromInt(1),
				Message:    pointer.StringPtr("second message"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation regex implicit count",
			template:   templateNeverValidateTwice,
			constraint: constraintNeverValidateTwice,
			object:     object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("[cdefinorst]+ [aegms]+"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation regex exact count",
			template:   templateNeverValidateTwice,
			constraint: constraintNeverValidateTwice,
			object:     object,
			assertions: []Assertion{{
				Violations: intStrFromInt(2),
				Message:    pointer.StringPtr("[cdefinorst]+ [aegms]+"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violations and one missing message",
			template:   templateNeverValidateTwice,
			constraint: constraintNeverValidateTwice,
			object:     object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("first message"),
			}, {
				Message: pointer.StringPtr("third message"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		// Invalid assertions
		{
			name:       "invalid IntOrStr",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: &intstr.IntOrString{Type: 3},
			}},
			want: CaseResult{
				Error: ErrInvalidYAML,
			},
		},
		{
			name:       "invalid IntOrStr string value",
			template:   templateNeverValidate,
			constraint: constraintNeverValidate,
			object:     object,
			assertions: []Assertion{{
				Violations: &intstr.IntOrString{Type: intstr.String, StrVal: "other"},
			}},
			want: CaseResult{
				Error: ErrInvalidYAML,
			},
		},
	}

	const (
		templateFile   = "template.yaml"
		constraintFile = "constraint.yaml"
		objectFile     = "object.yaml"
	)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			suite := &Suite{
				Tests: []Test{{
					Template:   templateFile,
					Constraint: constraintFile,
					Cases: []Case{{
						Object:     objectFile,
						Assertions: tc.assertions,
					}},
				}},
			}

			ctx := context.Background()

			runner := Runner{
				FS: fstest.MapFS{
					templateFile:   &fstest.MapFile{Data: []byte(tc.template)},
					constraintFile: &fstest.MapFile{Data: []byte(tc.constraint)},
					objectFile:     &fstest.MapFile{Data: []byte(tc.object)},
				},
				NewClient: NewOPAClient,
			}

			got := runner.Run(ctx, Filter{}, "", suite)

			want := SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{tc.want},
				}},
			}

			if diff := cmp.Diff(want, got, cmpopts.EquateErrors(), cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(SuiteResult{}, "Runtime"), cmpopts.IgnoreFields(TestResult{}, "Runtime"), cmpopts.IgnoreFields(CaseResult{}, "Runtime"),
			); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}
