package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanBookFileName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"【www.bookbao.com】三体.txt", "三体.txt"},
		{"【广告】测试书.txt", "测试书.txt"},
		{"  红楼梦  (精校版)  .TXT", "红楼梦 (精校版).txt"},
		{"bad/name?.txt", "badname.txt"},
		{"春光辉荒野（1~17） - powered by Discuz!.txt", "春光辉荒野（01--17）.txt"},
		{"海岸线之文学天地--Oursm Board - 琅環福地 - 连载合集区 - 春光辉荒野（1~17） - powered by Discuz!.txt", "春光辉荒野（01--17）.txt"},
		{"后来1-2.txt", "后来（01--02）.txt"},
		{"后来（1-2）.txt", "后来（01--02）.txt"},
		{"我同学的真实经历（01－02）.txt", "我同学的真实经历（01--02）.txt"},
		{"肥臀肉磨盘系列之电厂少妇(1-2).txt", "肥臀肉磨盘系列之电厂少妇（01--02）.txt"},
		{"惩罚任性的妻子 1-3.txt", "惩罚任性的妻子（01--03）.txt"},
		{"爱与欲（01－02）.txt", "爱与欲（01--02）.txt"},
		{"", "未命名图书.txt"},
	}
	for _, tt := range tests {
		got := CleanBookFileName(tt.in)
		if got != tt.want {
			t.Errorf("CleanBookFileName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMergeTitleWithFilenameRange(t *testing.T) {
	orig := "春光辉荒野（1~17） - powered by Discuz!.txt"
	contentTitle := "春光辉荒野（1~16）"
	got := mergeTitleWithFilenameRange(contentTitle, orig)
	want := "春光辉荒野（01--17）"
	if got != want {
		t.Fatalf("mergeTitleWithFilenameRange() = %q, want %q", got, want)
	}
}

func TestDecodeTextBytes(t *testing.T) {
	utf8BOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte("你好")...)
	text, enc := DecodeTextBytes(utf8BOM)
	if enc != "utf-8-bom" || text != "你好" {
		t.Fatalf("utf8 bom: got %q %q", enc, text)
	}

	raw := []byte("hello\x00world")
	text, _ = DecodeTextBytes(raw)
	if text != "helloworld" {
		t.Fatalf("expected helloworld, got %q", text)
	}
}

func TestRepairTextContent(t *testing.T) {
	input := "第一章\r\n\r\n正文内容\r\n\r\n\r\n\r\n\r\n请访问 www.example.com 下载更多\r\n第二段"
	out, stats := RepairTextContent(input)
	if stats.RemovedLines != 1 {
		t.Fatalf("expected 1 removed line, got %d", stats.RemovedLines)
	}
	if stringsContains(out, "www.example.com") {
		t.Fatalf("ad line should be removed: %q", out)
	}
	if !stringsContains(out, "第一章") || !stringsContains(out, "第二段") {
		t.Fatalf("content lost: %q", out)
	}
}

func TestRepairTextContentDiscuzForum(t *testing.T) {
	input := `海岸线之文学天地--Oursm Board - 琅環福地 - 连载合集区 - 春光辉荒野（1~17） - powered by Discuz!

&raquo; 头狼: 退出 | 短消息 | 控制面板 | 搜索 | 帮助

作者:标题: 春光辉荒野（1~17）上一主题 | 下一主题

萧舒
尊敬的原创者

积分 82
发贴 62
注册 2003-11-5
状态 离线 春光辉荒野（1~17）

春光辉荒野（1~16）

观天之道，执天之行，尽矣！

舅舅死了，舅舅死了？舅舅死了！

2004-7-14 01:56 AM

可打印版本 | 推荐给朋友 | 订阅主题 | 收藏主题

论坛跳转: 原创文学 > 琅環福地 > 连载合集区

Powered by Discuz! 3.1.2 &copy; 2001-04 Comsenz Technology Ltd
`
	out, _ := RepairTextContent(input)
	for _, bad := range []string{
		"powered by Discuz",
		"退出 | 短消息",
		"积分 82",
		"论坛跳转",
		"Comsenz Technology",
	} {
		if stringsContains(out, bad) {
			t.Fatalf("forum boilerplate should be removed, still has %q in:\n%s", bad, out)
		}
	}
	for _, good := range []string{"春光辉荒野（1~16）", "观天之道", "舅舅死了"} {
		if !stringsContains(out, good) {
			t.Fatalf("content lost %q in:\n%s", good, out)
		}
	}
}

func TestRepairBooks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	out := filepath.Join(dir, "out")
	os.MkdirAll(src, 0755)

	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte("第一章\r\n内容\r\n")...)
	os.WriteFile(filepath.Join(src, "【广告】测试书.txt"), content, 0644)

	if err := RepairBooks(src, out); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(out, "测试书.txt")); err != nil {
		t.Fatalf("repaired file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, RepairReportFile)); err != nil {
		t.Fatalf("report missing: %v", err)
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
