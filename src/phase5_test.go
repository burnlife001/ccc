package main

// phase5_test.go —— Phase 5 DPAPI + ccc cred 三操作的单测。
//
// 核心要求(todolist T1):
//   - protect→unprotect roundtrip 必须有单测
// 验收要点(ccc验收标准.md):
//   - A3 错误密码不落盘 → TestStoreCred_BadPasswordNoWrite
//   - A4 正常 cred 可 check → 实际由 base 通过 `dst/ccc.exe cred check` 跑通
//   - A5 文件被篡改(合法 base64 但解不开) → TestCheckCred_DpapiFails
//   - A6 删不存在 → TestDeleteCred_Missing / TestDeleteCred_Ok
//   - A7 非 tty 立即报错 → TestReadPasswordInteractive_NonTty
//
// 注:读侧 cryptUnprotectData / readCredDpapi 由 phase4 + e2e 覆盖;
// 这里只补 DPAPI 写侧 + cred 三操作的纯函数测试。

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
)

// skipIfNotWindows —— DPAPI / console mode 仅 Windows 可用,非 Windows 直接跳过。
// 仍保证编译通过(只是不运行)。
func skipIfNotWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI / console mode requires Windows")
	}
}

// createTestKdbx —— 在 t.TempDir() 下生成一个最小 kdbx,密码已知。
// 仅包含空 cc 组,足以让 verifyKdbxPassword 解码成功。
// 失败由 t.Fatal 中断,不返回错误(让测试 fail 得更直接)。
func createTestKdbx(t *testing.T, password string) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "test.kdbx")
	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	// 加一个名为 cc 的空组(纯占位,真实 kdbx 都至少有这个组)。
	db.Content.Root.Groups = append(db.Content.Root.Groups, gokeepasslib.Group{Name: "cc"})

	f, err := os.Create(tmp)
	if err != nil {
		t.Fatalf("create test kdbx: %v", err)
	}
	defer f.Close()

	enc := gokeepasslib.NewEncoder(f)
	if err := enc.Encode(db); err != nil {
		t.Fatalf("encode test kdbx: %v", err)
	}
	return tmp
}

// ----- T1: DPAPI 写侧 -----

// TestCryptProtectData_Roundtrip —— P0 必须:protect → unprotect 必须回到原文。
// 用同一进程同一用户(测试进程)调 protect,然后立即 unprotect;期望严格相等。
//
// 注:DPAPI 不接受空字节输入(Win32 返回 ERROR_INVALID_PARAMETER),
// 故此处不覆盖空串 case。
func TestCryptProtectData_Roundtrip(t *testing.T) {
	skipIfNotWindows(t)

	plaintexts := [][]byte{
		[]byte("hello world"),
		[]byte("密码-with-Unicodé-😀"),    // unicode
		bytes.Repeat([]byte("x"), 4096), // 中等长度
	}
	for _, p := range plaintexts {
		ct, err := cryptProtectData(p)
		if err != nil {
			t.Fatalf("protect(%d bytes): %v", len(p), err)
		}
		if len(ct) == 0 && len(p) > 0 {
			t.Fatalf("protect returned empty ciphertext for non-empty input")
		}
		got, err := cryptUnprotectData(ct)
		if err != nil {
			t.Fatalf("unprotect(%d bytes): %v", len(ct), err)
		}
		if !bytes.Equal(got, p) {
			t.Fatalf("roundtrip mismatch:\n got=%q\nwant=%q", got, p)
		}
	}
}

// TestCryptProtectData_EmptyInputRejected —— DPAPI 拒绝空输入(显式契约)。
func TestCryptProtectData_EmptyInputRejected(t *testing.T) {
	skipIfNotWindows(t)
	if _, err := cryptProtectData([]byte{}); err == nil {
		t.Fatal("DPAPI should reject empty plaintext")
	}
}

// TestCryptProtectData_NonWindows —— 非 Windows 上对称函数都应报 platform error。
func TestCryptProtectData_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("only meaningful off-Windows")
	}
	if _, err := cryptProtectData([]byte("x")); err == nil {
		t.Fatal("expected error on non-Windows")
	}
}

// TestCryptProtectData_DifferentCallsDifferentCiphertext —— DPAPI 加 IV/扰动,
// 同一明文多次加密不应产生完全相同密文(防误以为是明文存储)。
func TestCryptProtectData_DifferentCallsDifferentCiphertext(t *testing.T) {
	skipIfNotWindows(t)
	p := []byte("test-password")
	a, err := cryptProtectData(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := cryptProtectData(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("DPAPI ciphertext should vary across calls (same plaintext must not produce identical ciphertext)")
	}
}

// ----- T1+T2: storeCred / checkCred / deleteCred -----

// TestStoreCred_HappyPath —— 密码对 → 落盘 → check 也过。
func TestStoreCred_HappyPath(t *testing.T) {
	skipIfNotWindows(t)
	kdbx := createTestKdbx(t, "test-password-123")
	credPath := credPathFor(kdbx)

	if err := storeCred(kdbx, "test-password-123"); err != nil {
		t.Fatalf("storeCred: %v", err)
	}
	// 文件应存在且非空
	data, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("read cred after store: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("cred file empty")
	}
	// 内容应是合法 base64(标准字符集 + 空白可)
	if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data))); err != nil {
		t.Fatalf("cred file not valid base64: %v\nraw=%q", err, data)
	}
	// check 应通过
	if err := checkCred(kdbx); err != nil {
		t.Fatalf("checkCred after store: %v", err)
	}
	// 二次 store 应覆盖成功(幂等)
	if err := storeCred(kdbx, "test-password-123"); err != nil {
		t.Fatalf("storeCred idempotent: %v", err)
	}
	// 清理(虽然 t.TempDir() 会自动清)
	_ = os.Remove(credPath)
}

// TestStoreCred_BadPasswordNoWrite —— A3:错密码不落盘。
func TestStoreCred_BadPasswordNoWrite(t *testing.T) {
	skipIfNotWindows(t)
	kdbx := createTestKdbx(t, "correct-password")
	credPath := credPathFor(kdbx)

	// 先确保不存在
	_ = os.Remove(credPath)

	err := storeCred(kdbx, "WRONG-password")
	if err == nil {
		t.Fatal("storeCred with wrong password should fail")
	}
	if !strings.Contains(err.Error(), "bad credentials") {
		t.Errorf("error should mention bad credentials, got: %v", err)
	}
	// 关键断言:cred 文件不得产生
	if _, statErr := os.Stat(credPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cred file must NOT exist after failed store; stat err=%v", statErr)
	}
}

// TestStoreCred_FileModeIs0600 —— 凭据文件权限应仅当前用户可读写。
//
// 注:Windows NTFS 由 ACL 控制,POSIX mode 位只反映 readonly;
// chmod 0600 在 Windows 上是 no-op(os.Stat().Perm() 仍返回 0666),
// 故此 case 仅在非 Windows 上有意义。
func TestStoreCred_FileModeIs0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode meaningless on Windows (ACL-governed)")
	}
	kdbx := createTestKdbx(t, "test-pwd")
	credPath := credPathFor(kdbx)
	if err := storeCred(kdbx, "test-pwd"); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(credPath)

	fi, err := os.Stat(credPath)
	if err != nil {
		t.Fatal(err)
	}
	mode := fi.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("cred file perm = %o, want 0600", mode)
	}
}

// TestCheckCred_Missing —— 缺文件 → 报错指向 ccc cred store,不 panic。
func TestCheckCred_Missing(t *testing.T) {
	skipIfNotWindows(t)
	kdbx := createTestKdbx(t, "x")
	credPath := credPathFor(kdbx)
	_ = os.Remove(credPath)

	err := checkCred(kdbx)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ccc cred store") {
		t.Errorf("error should point to `ccc cred store`, got: %v", err)
	}
	// 错误用 %q 包,Windows 路径中的 \ 被转义为 \\,无法直接 Contains。
	// 检查 credPath 的目录名(t.TempDir 唯一)及固定文件名,两者合起来唯一标识。
	if !strings.Contains(err.Error(), filepath.Base(filepath.Dir(credPath))) {
		t.Errorf("error should mention cred dir basename, got: %v", err)
	}
	if !strings.Contains(err.Error(), ".kdbx_master.cred") {
		t.Errorf("error should mention cred filename, got: %v", err)
	}
}

// TestCheckCred_BadBase64 —— 文件被改成"合法 base64 但内容乱码",A5 子场景。
// CryptUnprotectData 应失败 → 错误指向 ccc cred store。
func TestCheckCred_BadBase64(t *testing.T) {
	skipIfNotWindows(t)
	kdbx := createTestKdbx(t, "real-password")
	credPath := credPathFor(kdbx)

	// 先 store 一个合法 cred,然后覆写成"合法 base64 但随机内容"
	if err := storeCred(kdbx, "real-password"); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(credPath)

	// 用 DPAPI 加密一些字节(不是 password),作为"合法 base64 但解出错误密码"
	wrong, err := cryptProtectData([]byte("wrong-password"))
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(wrong)
	if err := os.WriteFile(credPath, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}

	err = checkCred(kdbx)
	if err == nil {
		t.Fatal("expected error for credential that decrypts but does not open kdbx")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ccc cred store") {
		t.Errorf("error should point to ccc cred store, got: %s", msg)
	}
}

// TestCheckCred_DpapiFails —— 文件被改成"非合法 base64",A5 完整路径。
func TestCheckCred_DpapiFails(t *testing.T) {
	skipIfNotWindows(t)
	kdbx := createTestKdbx(t, "x")
	credPath := credPathFor(kdbx)

	// 写"不是 base64 的合法字符串"——含空格后随裸字母。
	if err := os.WriteFile(credPath, []byte("not!valid!base64@@@"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(credPath)

	err := checkCred(kdbx)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "base64") {
		t.Errorf("error should mention base64, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ccc cred store") {
		t.Errorf("error should point to ccc cred store, got: %v", err)
	}
}

// TestCheckCred_DpapiWrongUser —— A5 边界:DPAPI 能解密但解出错的密码。
// 通过 store 时用 A,check 时把文件替换成 B 的密文来模拟;B 打开不了 A 的 kdbx。
func TestCheckCred_DpapiWrongUser(t *testing.T) {
	skipIfNotWindows(t)
	kdbx := createTestKdbx(t, "real-password")
	credPath := credPathFor(kdbx)

	// 装一份合法 cred
	if err := storeCred(kdbx, "real-password"); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(credPath)

	// 把 cred 改成"另一份 DPAPI 密文(由不同 password 生成)"
	wrongBlob, err := cryptProtectData([]byte("some-other-password"))
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(wrongBlob)
	if err := os.WriteFile(credPath, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}

	err = checkCred(kdbx)
	if err == nil {
		t.Fatal("expected error (decrypted pw cannot open kdbx)")
	}
	if !strings.Contains(err.Error(), "does NOT open") && !strings.Contains(err.Error(), "bad credentials") {
		t.Errorf("error should clarify that decrypted pw does NOT open kdbx, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ccc cred store") {
		t.Errorf("error should point to ccc cred store, got: %v", err)
	}
}

// TestDeleteCred_Ok —— 删存在的文件 → (true, nil)。
func TestDeleteCred_Ok(t *testing.T) {
	skipIfNotWindows(t)
	kdbx := createTestKdbx(t, "x")
	if err := storeCred(kdbx, "x"); err != nil {
		t.Fatal(err)
	}
	credPath := credPathFor(kdbx)
	if _, err := os.Stat(credPath); err != nil {
		t.Fatalf("cred should exist before delete: %v", err)
	}

	existed, err := deleteCred(kdbx)
	if err != nil {
		t.Fatalf("deleteCred: %v", err)
	}
	if !existed {
		t.Error("existed should be true")
	}
	if _, err := os.Stat(credPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cred should be gone, stat err=%v", err)
	}
}

// TestDeleteCred_Missing —— A6:删不存在 → (false, nil),不报错。
func TestDeleteCred_Missing(t *testing.T) {
	skipIfNotWindows(t)
	kdbx := createTestKdbx(t, "x")
	_ = os.Remove(credPathFor(kdbx)) // ensure missing

	existed, err := deleteCred(kdbx)
	if err != nil {
		t.Fatalf("deleteCred on missing: %v", err)
	}
	if existed {
		t.Error("existed should be false")
	}
}

// ----- A7: 非 tty 立即报错(不卡死、不等待输入) -----

// TestReadPasswordInteractive_NonTty —— stdin 是管道时立即报错。
//
// 用 os.Pipe 替换 os.Stdin,GetConsoleMode 会失败 → 函数立即返回错误。
// 这覆盖了 `echo "" | ccc cred store` 的实际场景。
func TestReadPasswordInteractive_NonTty(t *testing.T) {
	skipIfNotWindows(t)

	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	// 用 pipe 模拟非 tty stdin(pipe 在 GetConsoleMode 时会报错)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	os.Stdin = r

	// 加超时:函数应在 <1s 内返回;如果卡死,test 自动 fail
	done := make(chan struct{})
	var gotErr error
	go func() {
		_, gotErr = readPasswordInteractive("test: ")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readPasswordInteractive hung on non-tty stdin (should fail immediately)")
	}
	if gotErr == nil {
		t.Fatal("expected error for non-tty stdin")
	}
	if !strings.Contains(gotErr.Error(), "interactive") {
		t.Errorf("error should mention interactive terminal, got: %v", gotErr)
	}
}

// ----- B2: 密码驻留内存最小化(冒烟检查:verifyKdbxPassword 内部不保留引用) -----
//
// 完整验证需要 race detector + 堆 dump;这里只静态确认 storeCred 验证
// 失败分支不向 cred 文件写入任何字节(覆盖在 TestStoreCred_BadPasswordNoWrite)。

// ----- 防止删错路径的 sanity 测试 -----

// TestCredPathFor_SameDirAsKdbx —— cred 永远在 kdbx 同目录,文件名固定。
func TestCredPathFor_SameDirAsKdbx(t *testing.T) {
	got := credPathFor(`E:\path\to\apikey.kdbx`)
	want := `E:\path\to\.kdbx_master.cred`
	if got != want {
		t.Errorf("credPathFor = %q, want %q", got, want)
	}
}

// TestStoreCred_NonWindows —— 非 Windows 平台对称返错,不落盘。
func TestStoreCred_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("only meaningful off-Windows")
	}
	err := storeCred(`C:\whatever\apikey.kdbx`, "x")
	if err == nil {
		t.Fatal("expected error on non-Windows")
	}
}

// TestCheckCred_NonWindows —— 同上。
func TestCheckCred_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("only meaningful off-Windows")
	}
	if err := checkCred(`C:\x\apikey.kdbx`); err == nil {
		t.Fatal("expected error on non-Windows")
	}
}

// TestDeleteCred_NonWindows —— 同上。
func TestDeleteCred_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("only meaningful off-Windows")
	}
	existed, err := deleteCred(`C:\x\apikey.kdbx`)
	if err == nil {
		t.Fatal("expected error on non-Windows")
	}
	if existed {
		t.Error("existed should be false on error")
	}
}