package service

import (
	"testing"
)

func TestSafeClassificationDir(t *testing.T) {
	for _, code := range []string{"B31/39", "C829.3/.7", "[I247.5]"} {
		if _, err := safeClassificationDir(code); err != nil {
			t.Fatalf("expected %q to be accepted: %v", code, err)
		}
	}
	for _, code := range []string{"../escape", "A/../escape", "A\\escape", "", "123"} {
		if _, err := safeClassificationDir(code); err == nil {
			t.Fatalf("expected %q to be rejected", code)
		}
	}
}

func TestCLCValidate_FinalCodeMatch(t *testing.T) {
	idx, err := LoadCLCIndex(DefaultCLCIndexPath)
	if err != nil {
		t.Skip(err)
	}
	// 中间层级类名可不一致，只要末级类号与分类号一致
	a := &BookAnalysis{
		Classification:     "B992.5",
		ClassificationPath: "B 哲学、宗教 > B9 宗教 > B99 错误名 > B992 错误 > B992.5 巫医、巫术",
	}
	v := idx.ValidateAnalysis(a)
	if !v.OK {
		t.Fatalf("expected valid, got %s", v.Reason)
	}
}

func TestCLCValidate_FinalCodeMismatch(t *testing.T) {
	idx, err := LoadCLCIndex(DefaultCLCIndexPath)
	if err != nil {
		t.Skip(err)
	}
	a := &BookAnalysis{
		Classification:     "B992.5",
		ClassificationPath: "B 哲学、宗教 > B9 宗教 > B99 术数、迷信 > B992 中国 > B992.3 占卜",
	}
	v := idx.ValidateAnalysis(a)
	if v.OK {
		t.Fatal("expected invalid when final code differs")
	}
}

func TestCLCValidate_InvalidB95_9(t *testing.T) {
	idx, err := LoadCLCIndex(DefaultCLCIndexPath)
	if err != nil {
		t.Skip(err)
	}
	a := &BookAnalysis{
		Classification:     "B95.9",
		ClassificationPath: "B 哲学、宗教 > B9 宗教 > B95 道教 > B95.9 道教经典",
	}
	v := idx.ValidateAnalysis(a)
	if v.OK {
		t.Fatal("expected invalid for fabricated B95.9")
	}
}

func TestParseBookAnalysis_Markdown(t *testing.T) {
	content := `**分类号**：B956.3
**最优分类路径**：B 哲学、宗教 > B9 宗教 > B95 道教 > B956 宗派 > B956.3 全真道
**书籍作者**：王重阳
**书籍国籍**：中国`
	a, err := parseBookAnalysis(content)
	if err != nil {
		t.Fatal(err)
	}
	if a.Classification != "B956.3" {
		t.Fatalf("classification=%q", a.Classification)
	}
	if a.ClassificationPath == "" {
		t.Fatal("path empty")
	}
	if a.Author == "" {
		t.Fatal("author empty")
	}
}
