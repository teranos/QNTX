//! Benchmarks for the AX lexer

use criterion::{black_box, criterion_group, criterion_main, Criterion, Throughput};
use qntx_parser::ax::Lexer;

fn bench_simple_query(c: &mut Criterion) {
    let input = "ALICE is author_of GitHub";

    let mut group = c.benchmark_group("lexer");
    group.throughput(Throughput::Bytes(input.len() as u64));

    group.bench_function("simple_query", |b| {
        b.iter(|| {
            let lexer = Lexer::new(black_box(input));
            let tokens: Vec<_> = lexer.collect();
            black_box(tokens)
        })
    });

    group.finish();
}

fn bench_complex_query(c: &mut Criterion) {
    let input = "ALICE BOB CHARLIE is author_of maintainer_of of GitHub Microsoft since 2024-01-01 by ADMIN";

    let mut group = c.benchmark_group("lexer");
    group.throughput(Throughput::Bytes(input.len() as u64));

    group.bench_function("complex_query", |b| {
        b.iter(|| {
            let lexer = Lexer::new(black_box(input));
            let tokens: Vec<_> = lexer.collect();
            black_box(tokens)
        })
    });

    group.finish();
}

fn bench_quoted_strings(c: &mut Criterion) {
    let input = "'hello world' 'foo bar' 'baz qux' 'test string'";

    let mut group = c.benchmark_group("lexer");
    group.throughput(Throughput::Bytes(input.len() as u64));

    group.bench_function("quoted_strings", |b| {
        b.iter(|| {
            let lexer = Lexer::new(black_box(input));
            let tokens: Vec<_> = lexer.collect();
            black_box(tokens)
        })
    });

    group.finish();
}

criterion_group!(
    benches,
    bench_simple_query,
    bench_complex_query,
    bench_quoted_strings
);
criterion_main!(benches);
