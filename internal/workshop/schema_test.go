package workshop

import "testing"

func TestValidateAgentJSON(t *testing.T) {
	t.Parallel()
	if err := ValidateAgentJSON([]byte(`{"name":"x","provider":"openai"}`)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentJSON([]byte(`{}`)); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateSkillMarkdown_Frontmatter(t *testing.T) {
	t.Parallel()
	good := "---\nname: test\ndescription: d\n---\n# Body\n"
	if err := ValidateSkillMarkdown(good); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSkillMarkdown("---\n---\n"); err == nil {
		t.Fatal("expected error for empty frontmatter fields")
	}
}
