//! gitlab-glfm-markdown の描画 (glfm.rs・formatter.rs) を、WebAssembly から呼ぶための入口。
//!
//! glfm.rs と formatter.rs は gem の写しで、変えない (engine.lock.json の sha256 と照合する)。
//! gem の lib.rs は Ruby との橋渡し (magnus) なので使わず、ここに C の呼び出し規約の入口だけを置く。
//! JavaScript 側 (lib/engine.mjs) の使い方:
//!   1. glfm_alloc で入力の領域を取り、UTF-8 の Markdown とオプションの JSON を書く
//!   2. glfm_render を呼ぶ。戻り値は結果の領域の先頭
//!      [0]      状態 (0 = 成功、1 = 誤り)
//!      [1..5]   本体の長さ (u32、リトルエンディアン)
//!      [5..]    本体 (成功なら HTML、誤りなら説明の文。どちらも UTF-8)
//!   3. 読み終えたら、入力と結果の領域を glfm_free で返す (結果の長さは 5 + 本体の長さ)
mod formatter;
mod glfm;

use std::alloc::{alloc, dealloc, Layout};

use glfm::{render, RenderOptions};

const HEADER: usize = 5;

/// len バイトの領域を取る。len が 0 のときは、返す必要のない番兵を返す。
#[no_mangle]
pub extern "C" fn glfm_alloc(len: usize) -> *mut u8 {
    if len == 0 {
        return std::ptr::NonNull::<u8>::dangling().as_ptr();
    }
    let Ok(layout) = Layout::from_size_align(len, 1) else {
        return std::ptr::null_mut();
    };
    // SAFETY: len は 0 でなく、layout は上で検査した。
    unsafe { alloc(layout) }
}

/// glfm_alloc か glfm_render が返した領域を返す。len は取ったときと同じ値。
///
/// # Safety
/// ptr は glfm_alloc(len) か、glfm_render の戻り値 (len は 5 + 本体の長さ) であること。
#[no_mangle]
pub unsafe extern "C" fn glfm_free(ptr: *mut u8, len: usize) {
    if ptr.is_null() || len == 0 {
        return;
    }
    if let Ok(layout) = Layout::from_size_align(len, 1) {
        dealloc(ptr, layout);
    }
}

fn result(status: u8, body: &[u8]) -> *mut u8 {
    let (Ok(body_len), Some(total)) = (u32::try_from(body.len()), HEADER.checked_add(body.len()))
    else {
        return result(1, "結果が大きすぎます。".as_bytes());
    };
    let ptr = glfm_alloc(total);
    if ptr.is_null() {
        return ptr;
    }
    // SAFETY: ptr は total (HEADER + body.len()) バイトの、取ったばかりの領域。
    unsafe {
        *ptr = status;
        std::ptr::copy_nonoverlapping(body_len.to_le_bytes().as_ptr(), ptr.add(1), 4);
        std::ptr::copy_nonoverlapping(body.as_ptr(), ptr.add(HEADER), body.len());
    }
    ptr
}

/// Markdown を、GitLab の整形器の HTML にする。オプションは gem の RenderOptions と同じ名前の JSON。
///
/// # Safety
/// text と options は、それぞれ text_len・options_len バイトの読める領域を指すこと (ヌルは誤りとして返す)。
#[no_mangle]
pub unsafe extern "C" fn glfm_render(
    text: *const u8,
    text_len: usize,
    options: *const u8,
    options_len: usize,
) -> *mut u8 {
    // 長さが 0 でも、ヌルのポインタからスライスは作れない。
    if text.is_null() || options.is_null() {
        return result(1, "入力の領域がありません。".as_bytes());
    }
    let text = std::slice::from_raw_parts(text, text_len);
    let options = std::slice::from_raw_parts(options, options_len);
    let Ok(text) = std::str::from_utf8(text) else {
        return result(1, "Markdown が UTF-8 ではありません。".as_bytes());
    };
    let options: RenderOptions = match serde_json::from_slice(options) {
        Ok(options) => options,
        Err(error) => return result(1, format!("オプションを読めません: {error}").as_bytes()),
    };
    result(0, render(text, &options).as_bytes())
}
