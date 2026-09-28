use rwkv_tokenizer::WorldTokenizer;
use std::{env, fs, hint::black_box, time::Instant};
fn main() {
    let a: Vec<String> = env::args().collect();
    let text = fs::read_to_string(&a[2]).unwrap();
    let tok = WorldTokenizer::new(Some(&a[3])).unwrap();
    if a[1] == "dump" {
        let ids = tok.encode(&text);
        let mut bytes = (ids.len() as u32).to_le_bytes().to_vec();
        for id in ids {
            bytes.extend((id as u32).to_le_bytes());
        }
        fs::write(&a[4], bytes).unwrap();
        return;
    }
    let iters: usize = a[4].parse().unwrap();
    let n: usize = a[5].parse().unwrap();
    let mut texts = Vec::new();
    let mut start = 0;
    let size = text.len() / n.max(1);
    if n > 1 && text.len() >= 2 * n {
        while texts.len() < n - 1 {
            let want = start + size;
            if want >= text.len() {
                break;
            }
            match text.as_bytes()[want..].iter().position(|&b| b == b'\n') {
                Some(p) => {
                    let end = want + p + 1;
                    texts.push(text[start..end].to_string());
                    start = end;
                }
                None => break,
            }
        }
    }
    texts.push(text[start..].to_string());
    let run = || {
        if n == 0 {
            black_box(tok.encode(black_box(&text))).len()
        } else {
            black_box(tok.encode_batch(black_box(texts.clone())))
                .iter()
                .map(Vec::len)
                .sum()
        }
    };
    for _ in 0..3 {
        black_box(run());
    }
    let t = Instant::now();
    let mut count = 0;
    for _ in 0..iters {
        count = black_box(run());
    }
    println!(
        "{} bytes {} tokens {:.1} MB/s",
        text.len(),
        count,
        text.len() as f64 * iters as f64 / t.elapsed().as_secs_f64() / 1e6
    );
}
