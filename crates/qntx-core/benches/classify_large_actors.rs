//! Benchmark: classification performance with large actor sets (simulating sigma inheritance).
//!
//! Tests the scenario where a distilled sigma carries 100K inherited actors
//! and a fresh claim arrives that conflicts with it.

use std::time::Instant;

use qntx_core::classify::{ClaimGroup, ClaimInput, ClassifyInput, SmartClassifier, TemporalConfig};

fn make_sigma_claim(num_actors: usize, now: i64) -> ClaimInput {
    // Simulate a sigma with many inherited actors — the claim carries one actor
    // but the realistic scenario is: many claims in the same group, each from
    // a unique actor (as levi would produce before distillation).
    ClaimInput {
        subject: "SIGMA-SUBJECT".to_string(),
        predicate: "crawled".to_string(),
        context: "RETICULUM".to_string(),
        // The sigma's "highest" actor determines its credibility
        actor: format!("system:distill-with-{}-actors", num_actors),
        timestamp_ms: now - 3_600_000, // 1 hour ago
        source_id: "sigma-001".to_string(),
    }
}

fn make_fresh_claim(actor: &str, now: i64) -> ClaimInput {
    ClaimInput {
        subject: "SIGMA-SUBJECT".to_string(),
        predicate: "crawled".to_string(),
        context: "RETICULUM".to_string(),
        actor: actor.to_string(),
        timestamp_ms: now,
        source_id: "fresh-001".to_string(),
    }
}

fn bench_many_claims_in_group(num_claims: usize) {
    let now = 1_000_000_000_i64;
    let config = TemporalConfig::default();
    let classifier = SmartClassifier::new(config.clone());

    // Simulate a group with num_claims claims, each from a unique actor
    // (this is what happens before distillation — many actors on same SPC)
    let claims: Vec<ClaimInput> = (0..num_claims)
        .map(|i| ClaimInput {
            subject: "SIGMA-SUBJECT".to_string(),
            predicate: "crawled".to_string(),
            context: "RETICULUM".to_string(),
            actor: format!("levi-node-{:06}", i),
            timestamp_ms: now - (num_claims as i64 - i as i64) * 1000,
            source_id: format!("as-{:06}", i),
        })
        .collect();

    let input = ClassifyInput {
        claim_groups: vec![ClaimGroup {
            key: "SIGMA-SUBJECT|crawled|RETICULUM".to_string(),
            claims,
        }],
        config,
        now_ms: now,
    };

    // Warm up
    let _ = classifier.classify(&input);

    // Measure
    let iterations = 100;
    let start = Instant::now();
    for _ in 0..iterations {
        let _ = classifier.classify(&input);
    }
    let elapsed = start.elapsed();

    println!(
        "  {:>7} claims/group: {:>8.2}µs/classify ({} iterations)",
        num_claims,
        elapsed.as_micros() as f64 / iterations as f64,
        iterations
    );
}

fn bench_supersession_with_large_group(num_claims: usize) {
    let now = 1_000_000_000_i64;
    let config = TemporalConfig::default();
    let classifier = SmartClassifier::new(config.clone());

    // N-1 levi claims + 1 human claim → supersession
    let mut claims: Vec<ClaimInput> = (0..num_claims - 1)
        .map(|i| ClaimInput {
            subject: "SIGMA-SUBJECT".to_string(),
            predicate: "crawled".to_string(),
            context: "RETICULUM".to_string(),
            actor: format!("levi-node-{:06}", i),
            timestamp_ms: now - (num_claims as i64 - i as i64) * 1000,
            source_id: format!("as-{:06}", i),
        })
        .collect();

    claims.push(ClaimInput {
        subject: "SIGMA-SUBJECT".to_string(),
        predicate: "crawled".to_string(),
        context: "RETICULUM".to_string(),
        actor: "human:brandon".to_string(),
        timestamp_ms: now,
        source_id: "as-human".to_string(),
    });

    let input = ClassifyInput {
        claim_groups: vec![ClaimGroup {
            key: "SIGMA-SUBJECT|crawled|RETICULUM".to_string(),
            claims,
        }],
        config,
        now_ms: now,
    };

    // Warm up
    let _ = classifier.classify(&input);

    // Measure
    let iterations = 100;
    let start = Instant::now();
    for _ in 0..iterations {
        let _ = classifier.classify(&input);
    }
    let elapsed = start.elapsed();

    let output = classifier.classify(&input);
    let conflict = &output.conflicts[0];

    println!(
        "  {:>7} claims (supersession): {:>8.2}µs/classify | resolved={} type={:?}",
        num_claims,
        elapsed.as_micros() as f64 / iterations as f64,
        output.resolved_source_ids.len(),
        conflict.conflict_type
    );
}

fn main() {
    println!("=== Classification benchmark: large actor groups ===\n");

    println!("Scenario 1: All external actors (levi nodes), same SPC group");
    bench_many_claims_in_group(10);
    bench_many_claims_in_group(100);
    bench_many_claims_in_group(1_000);
    bench_many_claims_in_group(10_000);
    bench_many_claims_in_group(100_000);

    println!("\nScenario 2: N-1 external + 1 human (supersession)");
    bench_supersession_with_large_group(10);
    bench_supersession_with_large_group(100);
    bench_supersession_with_large_group(1_000);
    bench_supersession_with_large_group(10_000);
    bench_supersession_with_large_group(100_000);
}
