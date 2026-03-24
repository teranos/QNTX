/// Inference signal computation — entropy, confidence, and anomaly detection
/// from per-token probability distributions.
///
/// Signals computed per-token:
///   - entropy: H = -Σ p_i * log2(p_i) over the top-N distribution
///   - confidence: probability of the selected (top-1) token
///   - top_gap: p_top1 - p_top2 — how decisive the selection was
///
/// Signals computed per-generation (aggregate):
///   - mean/max/min entropy and confidence
///   - entropy spike positions (tokens where entropy exceeds mean + 1 stddev)
///   - low confidence spans (runs where confidence < threshold)
module infer.signals;

import infer.http : TokenProbEntry;
import std.conv : to;

/// Per-token signal values.
struct TokenSignal {
    double entropy;     // bits
    double confidence;  // 0.0 - 1.0
    double topGap;      // p1 - p2
}

/// Aggregate signals for an entire generation.
struct GenerationSignals {
    // Per-token
    TokenSignal[] tokens;

    // Aggregate entropy
    double meanEntropy;
    double maxEntropy;
    double minEntropy;

    // Aggregate confidence
    double meanConfidence;
    double minConfidence;

    // Anomaly positions
    int[] entropySpikes;     // Token indices where entropy > mean + 1 stddev
    int[][] lowConfSpans;    // [start, end] ranges where confidence < threshold

    // Counts
    int tokenCount;
}

/// Compute all signals from the token probability data.
GenerationSignals computeSignals(const TokenProbEntry[] probs, double confThreshold = 0.3) {
    GenerationSignals result;
    result.tokenCount = cast(int)probs.length;

    if (probs.length == 0) return result;

    result.tokens.length = probs.length;
    result.minEntropy = double.max;
    result.minConfidence = double.max;

    double entropySum = 0.0;
    double confSum = 0.0;

    // Pass 1: compute per-token signals
    foreach (i, ref entry; probs) {
        auto sig = computeTokenSignal(entry);
        result.tokens[i] = sig;

        entropySum += sig.entropy;
        confSum += sig.confidence;

        if (sig.entropy > result.maxEntropy) result.maxEntropy = sig.entropy;
        if (sig.entropy < result.minEntropy) result.minEntropy = sig.entropy;
        if (sig.confidence < result.minConfidence) result.minConfidence = sig.confidence;
    }

    result.meanEntropy = entropySum / probs.length;
    result.meanConfidence = confSum / probs.length;

    // Pass 2: compute stddev for entropy spike detection
    double varianceSum = 0.0;
    foreach (ref sig; result.tokens) {
        double diff = sig.entropy - result.meanEntropy;
        varianceSum += diff * diff;
    }
    double stddev = sqrt(varianceSum / probs.length);
    double spikeThreshold = result.meanEntropy + stddev;

    // Pass 3: detect entropy spikes and low-confidence spans
    bool inLowConf = false;
    int lowConfStart = 0;

    foreach (i, ref sig; result.tokens) {
        // Entropy spikes
        if (sig.entropy > spikeThreshold) {
            result.entropySpikes ~= cast(int)i;
        }

        // Low confidence spans
        if (sig.confidence < confThreshold) {
            if (!inLowConf) {
                inLowConf = true;
                lowConfStart = cast(int)i;
            }
        } else {
            if (inLowConf) {
                result.lowConfSpans ~= [lowConfStart, cast(int)i - 1];
                inLowConf = false;
            }
        }
    }
    // Close trailing span
    if (inLowConf) {
        result.lowConfSpans ~= [lowConfStart, cast(int)result.tokens.length - 1];
    }

    return result;
}

/// Compute entropy and confidence for a single token's probability distribution.
private TokenSignal computeTokenSignal(ref const TokenProbEntry entry) {
    TokenSignal sig;
    sig.confidence = entry.prob;

    if (entry.topProbs.length == 0) {
        // No distribution data — entropy is 0 (fully certain)
        sig.entropy = 0.0;
        sig.topGap = entry.prob;
        return sig;
    }

    // Entropy: H = -Σ p_i * log2(p_i) over the observed top-N
    double h = 0.0;
    foreach (ref tp; entry.topProbs) {
        if (tp.prob > 0.0) {
            h -= tp.prob * log2(tp.prob);
        }
    }
    sig.entropy = h;

    // Top gap: difference between top-1 and top-2
    if (entry.topProbs.length >= 2) {
        sig.topGap = entry.topProbs[0].prob - entry.topProbs[1].prob;
    } else {
        sig.topGap = entry.topProbs[0].prob;
    }

    return sig;
}

/// Format signals as a string map suitable for attestation attributes.
string[string] signalsToAttributes(ref const GenerationSignals sigs) {
    string[string] attrs;

    attrs["token_count"] = sigs.tokenCount.to!string;
    attrs["mean_entropy"] = formatNum(sigs.meanEntropy);
    attrs["max_entropy"] = formatNum(sigs.maxEntropy);
    attrs["min_entropy"] = formatNum(sigs.minEntropy);
    attrs["mean_confidence"] = formatNum(sigs.meanConfidence);
    attrs["min_confidence"] = formatNum(sigs.minConfidence);
    attrs["entropy_spike_count"] = (cast(int)sigs.entropySpikes.length).to!string;
    attrs["low_conf_span_count"] = (cast(int)sigs.lowConfSpans.length).to!string;

    // Encode spike positions as comma-separated
    if (sigs.entropySpikes.length > 0) {
        string spikes;
        foreach (i, pos; sigs.entropySpikes) {
            if (i > 0) spikes ~= ",";
            spikes ~= pos.to!string;
        }
        attrs["entropy_spike_positions"] = spikes;
    }

    // Encode low-confidence spans as "start-end,start-end"
    if (sigs.lowConfSpans.length > 0) {
        string spans;
        foreach (i, span; sigs.lowConfSpans) {
            if (i > 0) spans ~= ",";
            spans ~= span[0].to!string ~ "-" ~ span[1].to!string;
        }
        attrs["low_conf_spans"] = spans;
    }

    return attrs;
}

// ---------------------------------------------------------------------------
// Math helpers (avoid importing std.math for the whole module)
// ---------------------------------------------------------------------------

private double log2(double x) {
    import std.math : log2_ = log2;
    return log2_(x);
}

private double sqrt(double x) {
    import std.math : sqrt_ = sqrt;
    return sqrt_(x);
}

private string formatNum(double v) {
    import std.format : format;
    return format("%.6f", v);
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

unittest {
    import infer.http : TokenProbEntry, TokenProb;

    // Test 1: single highly-confident token
    TokenProbEntry confident;
    confident.token = "Paris";
    confident.prob = 0.95;
    confident.topProbs = [
        TokenProb("Paris", 0.95, 0),
        TokenProb("London", 0.03, 0),
        TokenProb("Berlin", 0.02, 0),
    ];

    auto sigs = computeSignals([confident]);
    assert(sigs.tokenCount == 1);
    assert(sigs.meanConfidence > 0.9);
    assert(sigs.meanEntropy < 0.5); // Low entropy = high confidence
    assert(sigs.entropySpikes.length == 0);

    // Test 2: uncertain token
    TokenProbEntry uncertain;
    uncertain.token = "maybe";
    uncertain.prob = 0.35;
    uncertain.topProbs = [
        TokenProb("maybe", 0.35, 0),
        TokenProb("perhaps", 0.30, 0),
        TokenProb("possibly", 0.20, 0),
        TokenProb("likely", 0.15, 0),
    ];

    auto sigs2 = computeSignals([uncertain]);
    assert(sigs2.meanEntropy > 1.0); // Higher entropy
    assert(sigs2.meanConfidence < 0.4);

    // Test 3: mixed sequence with one spike
    auto mixed = [confident, confident, uncertain, confident];
    auto sigs3 = computeSignals(mixed);
    assert(sigs3.tokenCount == 4);
    assert(sigs3.entropySpikes.length >= 1); // The uncertain token should spike

    // Test 4: signalsToAttributes
    auto attrs = signalsToAttributes(sigs3);
    assert("mean_entropy" in attrs);
    assert("token_count" in attrs);
    assert(attrs["token_count"] == "4");

    // Test 5: empty input
    auto empty = computeSignals([]);
    assert(empty.tokenCount == 0);
}
