// View model for the chat screen. Owns the engine host, drives generation,
// and exposes per-token signals for the chat UI and the nebula view.

import Foundation
import SwiftUI
import ScryKit

@MainActor
public final class ChatViewModel: ObservableObject {
    public enum Phase: Sendable {
        case unloaded
        case loading
        case ready
        case generating
        case error(String)
    }

    // A single visible "turn" — user prompt + assistant response. The
    // assistant response is a sequence of TokenSignals so the UI can color
    // each token by confidence and respond to taps.
    public struct Turn: Identifiable, Sendable {
        public let id = UUID()
        public var userText: String
        public var signals: [TokenSignal]   // grows as tokens stream in
        // The completed result; nil while generating.
        public var result: GenerationResult?
    }

    @Published public private(set) var phase: Phase = .unloaded
    @Published public private(set) var modelName: String?
    @Published public private(set) var turns: [Turn] = []
    @Published public var prompt: String = ""

    // Selected token for the fan-out view. nil = chat-only mode.
    @Published public var focusedTokenIndex: (turnId: UUID, tokenIdx: Int)?

    private let engine = ScryEngineHost()
    private var systemPrompt: String = "You are a helpful assistant."
    private var vocabPositionsPtr: UnsafePointer<Float>?

    public init() {}

    public func loadModel(at url: URL) async {
        phase = .loading
        do {
            try await engine.loadModel(at: url)
            modelName = await engine.modelName
            // Cache the vocab positions pointer once. It's stable until unload,
            // and the underlying memory is owned by the C++ engine.
            vocabPositionsPtr = await engine.vocabPositions3D()
            phase = .ready
        } catch {
            phase = .error("load failed: \(error.localizedDescription)")
        }
    }

    public func send() async {
        let text = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        prompt = ""
        var turn = Turn(userText: text, signals: [], result: nil)
        turns.append(turn)
        let turnId = turn.id
        phase = .generating

        // Build full message history.
        var messages: [ChatMessage] = [
            ChatMessage(role: .system, content: systemPrompt)
        ]
        for t in turns {
            messages.append(ChatMessage(role: .user, content: t.userText))
            if let r = t.result {
                messages.append(ChatMessage(role: .assistant, content: r.content))
            }
        }

        do {
            let stream = await engine.generate(messages: messages)
            for try await event in stream {
                switch event {
                case .token(_, let signal):
                    if let idx = turns.firstIndex(where: { $0.id == turnId }) {
                        turns[idx].signals.append(signal)
                    }
                case .finished(let result):
                    if let idx = turns.firstIndex(where: { $0.id == turnId }) {
                        turns[idx].result = result
                        // Replace signals with the canonical ones from the
                        // result — they have accurate kvPosition.
                        turns[idx].signals = result.signals
                    }
                }
            }
            phase = .ready
        } catch {
            phase = .error("generation failed: \(error.localizedDescription)")
        }
    }

    // RDR: branch from the focused token, replacing it with `alternative`.
    // Replaces all subsequent tokens in the turn with the new generation.
    public func branch(from turnId: UUID, atTokenIndex idx: Int, alternative: TokenCandidate) async {
        guard let turnIdx = turns.firstIndex(where: { $0.id == turnId }),
              let result = turns[turnIdx].result,
              idx < turns[turnIdx].signals.count else { return }

        let forkPosition = turns[turnIdx].signals[idx].kvPosition
        // Truncate the displayed signals to the tokens before the forked one.
        turns[turnIdx].signals = Array(turns[turnIdx].signals.prefix(idx))
        phase = .generating

        do {
            let stream = await engine.fork(from: result,
                                            forkPosition: forkPosition,
                                            forkTokenId: alternative.tokenId)
            for try await event in stream {
                switch event {
                case .token(_, let signal):
                    turns[turnIdx].signals.append(signal)
                case .finished(let newResult):
                    turns[turnIdx].result = newResult
                    // Reconstruct full token list: original prefix + new signals.
                    let prefix = Array(turns[turnIdx].signals.prefix(idx))
                    turns[turnIdx].signals = prefix + newResult.signals
                }
            }
            phase = .ready
        } catch {
            phase = .error("fork failed: \(error.localizedDescription)")
        }
    }

    // Vocab positions for nebula rendering. Returns the positions for the
    // top-K candidates of the focused signal as ParticleSpecs.
    public func focusedParticles(maxAlpha: Float = 1.0) -> [ParticleSpec] {
        guard let focus = focusedTokenIndex,
              let turn = turns.first(where: { $0.id == focus.turnId }),
              focus.tokenIdx < turn.signals.count else {
            return []
        }
        let signal = turn.signals[focus.tokenIdx]
        guard let positionsPtr = vocabPositionsPtr else { return [] }

        var specs: [ParticleSpec] = []
        specs.reserveCapacity(signal.topK.count + 1)

        // Chosen token highlighted larger and brighter.
        let chosenSpec = makeParticle(tokenId: signal.tokenId,
                                       probability: signal.confidence,
                                       positions: positionsPtr,
                                       alphaScale: maxAlpha,
                                       sizeBase: 0.04,
                                       boost: 1.3)
        if let s = chosenSpec { specs.append(s) }
        for cand in signal.topK where cand.tokenId != signal.tokenId {
            if let s = makeParticle(tokenId: cand.tokenId,
                                    probability: cand.probability,
                                    positions: positionsPtr,
                                    alphaScale: maxAlpha,
                                    sizeBase: 0.025,
                                    boost: 1.0) {
                specs.append(s)
            }
        }
        return specs
    }

    private func makeParticle(tokenId: Int32,
                              probability: Float,
                              positions: UnsafePointer<Float>,
                              alphaScale: Float,
                              sizeBase: Float,
                              boost: Float) -> ParticleSpec? {
        let idx = Int(tokenId)
        let stride = 6
        // Unsafe — positions is vocabSize × 6. Caller holds vocabSize.
        let px = positions[idx * stride + 0]
        let py = positions[idx * stride + 1]
        let pz = positions[idx * stride + 2]
        let cr = positions[idx * stride + 3]
        let cg = positions[idx * stride + 4]
        let cb = positions[idx * stride + 5]

        let alpha = min(1.0, sqrt(probability) * boost) * alphaScale
        let size = sizeBase + sizeBase * 4 * sqrt(probability) * boost
        return ParticleSpec(tokenId: tokenId,
                             position: SIMD3<Float>(px, py, pz),
                             color: SIMD3<Float>(cr, cg, cb),
                             alpha: alpha,
                             size: size)
    }

    public func tokenText(for id: Int32) async -> String? {
        await engine.tokenText(for: id)
    }
}
