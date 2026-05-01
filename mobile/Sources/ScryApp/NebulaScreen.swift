// Fullscreen 3D fan-out view. Shown when the user taps a token in chat.
// The chosen token sits at the center of focus; runner-up alternatives
// fan out in PCA position space, sized and alpha'd by probability. Tap
// any particle to branch generation from that point (RDR).

import SwiftUI
import ScryKit

public struct NebulaScreen: View {
    @ObservedObject var vm: ChatViewModel
    let turnId: UUID
    let tokenIdx: Int
    let onDismiss: () -> Void

    @State private var particles: [ParticleSpec] = []
    @State private var hoveredCandidate: TokenCandidate?
    @State private var branching = false

    public var body: some View {
        ZStack(alignment: .topTrailing) {
            NebulaView(particles: $particles) { tokenId in
                handlePick(tokenId: tokenId)
            }
            .ignoresSafeArea()

            VStack(alignment: .trailing, spacing: 12) {
                Button(action: onDismiss) {
                    Image(systemName: "xmark.circle.fill")
                        .font(.title)
                        .foregroundStyle(.white.opacity(0.8))
                }
                if let signal = currentSignal {
                    SignalLegend(signal: signal)
                }
                Spacer()
                if let cand = hoveredCandidate {
                    CandidateChip(candidate: cand) {
                        Task { await branchTo(cand) }
                    }
                    .padding(.bottom, 24)
                }
            }
            .padding()

            if branching {
                Color.black.opacity(0.4).ignoresSafeArea()
                ProgressView("rewriting…")
                    .tint(.white)
                    .foregroundStyle(.white)
            }
        }
        .background(.black)
        .onAppear { rebuildParticles() }
        .onChange(of: vm.turns.first(where: { $0.id == turnId })?.signals.count) { _ in
            rebuildParticles()
        }
    }

    private var currentSignal: TokenSignal? {
        guard let turn = vm.turns.first(where: { $0.id == turnId }),
              tokenIdx < turn.signals.count else { return nil }
        return turn.signals[tokenIdx]
    }

    private func rebuildParticles() {
        particles = vm.focusedParticles()
    }

    private func handlePick(tokenId: Int32) {
        guard let signal = currentSignal else { return }
        if let cand = signal.topK.first(where: { $0.tokenId == tokenId }) {
            hoveredCandidate = cand
        }
    }

    private func branchTo(_ candidate: TokenCandidate) async {
        branching = true
        await vm.branch(from: turnId, atTokenIndex: tokenIdx, alternative: candidate)
        branching = false
        onDismiss()
    }
}

private struct SignalLegend: View {
    let signal: TokenSignal
    var body: some View {
        VStack(alignment: .trailing, spacing: 2) {
            Text(signal.text)
                .font(.title2.monospaced())
                .foregroundStyle(.white)
            Text("p=\(String(format: "%.3f", signal.confidence))  H=\(String(format: "%.2f", signal.entropy))b  Δ=\(String(format: "%.2f", signal.topGap))")
                .font(.caption.monospaced())
                .foregroundStyle(.white.opacity(0.7))
        }
        .padding(8)
        .background(.black.opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }
}

private struct CandidateChip: View {
    let candidate: TokenCandidate
    let onBranch: () -> Void
    var body: some View {
        Button(action: onBranch) {
            HStack(spacing: 8) {
                Text(candidate.text)
                    .font(.title3.monospaced())
                    .foregroundStyle(.white)
                Text(String(format: "%.2f", candidate.probability))
                    .font(.caption.monospaced())
                    .foregroundStyle(.white.opacity(0.7))
                Image(systemName: "arrow.triangle.branch")
                    .foregroundStyle(.white)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .background(.white.opacity(0.15))
            .clipShape(Capsule())
        }
    }
}
