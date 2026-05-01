// First-launch model picker. Shows the curated catalog, indicates which
// are already on disk, downloads the chosen one with a progress bar.
// Allows pasting a custom GGUF URL (matches scry's model-agnostic ethos).

import SwiftUI
import ScryKit

public struct ModelPickerView: View {
    @Binding public var selectedModelURL: URL?
    @State private var downloading: CatalogModel?
    @State private var progress: Double = 0
    @State private var customURL: String = ""
    @State private var error: String?

    public init(selectedModelURL: Binding<URL?>) {
        self._selectedModelURL = selectedModelURL
    }

    public var body: some View {
        NavigationStack {
            List {
                Section("curated") {
                    ForEach(ModelCatalog.curated) { model in
                        modelRow(model)
                    }
                }
                Section("custom GGUF URL") {
                    TextField("https://...", text: $customURL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Button("download & use") { downloadCustom() }
                        .disabled(customURL.isEmpty)
                }
                if let error = error {
                    Section("error") {
                        Text(error).foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("scry models")
        }
    }

    private func modelRow(_ model: CatalogModel) -> some View {
        let downloaded = ModelDownloader.isDownloaded(model)
        return HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(model.displayName)
                    .font(.body)
                Text(byteString(model.approximateBytes))
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if downloading?.id == model.id {
                ProgressView(value: progress)
                    .frame(width: 80)
            } else if downloaded {
                Button("use") {
                    if let url = try? ModelDownloader.localURL(for: model) {
                        selectedModelURL = url
                    }
                }
                .buttonStyle(.borderedProminent)
            } else {
                Button("download") { download(model) }
                    .buttonStyle(.bordered)
            }
        }
    }

    private func download(_ model: CatalogModel) {
        downloading = model
        progress = 0
        error = nil
        Task {
            let downloader = ModelDownloader()
            for await event in await downloader.download(model) {
                switch event {
                case .starting:
                    progress = 0
                case .downloading(let received, let expected):
                    progress = expected > 0
                        ? Double(received) / Double(expected)
                        : 0
                case .completed(let url):
                    downloading = nil
                    selectedModelURL = url
                case .failed(let err):
                    downloading = nil
                    error = err.localizedDescription
                }
            }
        }
    }

    private func downloadCustom() {
        guard let url = URL(string: customURL) else {
            error = "invalid URL: \(customURL)"
            return
        }
        let fileName = url.lastPathComponent.isEmpty
            ? "custom-\(UUID().uuidString.prefix(8)).gguf"
            : url.lastPathComponent
        let model = CatalogModel(
            displayName: fileName,
            fileName: String(fileName),
            url: url,
            approximateBytes: 0
        )
        download(model)
    }

    private func byteString(_ bytes: Int64) -> String {
        let bcf = ByteCountFormatter()
        bcf.allowedUnits = [.useGB, .useMB]
        bcf.countStyle = .file
        return bcf.string(fromByteCount: bytes)
    }
}
