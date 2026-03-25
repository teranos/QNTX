import Foundation
import GRPC
import NIOCore
import SwiftProtobuf

/// DomainPluginService implementation for Swift + Metal visualization.
final class SwiftMetalPlugin: Protocol_DomainPluginServiceProvider {
    var interceptors: Protocol_DomainPluginServiceServerInterceptorFactoryProtocol? { nil }

    private var config: [String: String] = [:]
    private var atsEndpoint: String = ""
    private var authToken: String = ""
    private let renderer = MetalRenderer()

    // MARK: - Metadata

    func metadata(
        request: Protocol_Empty,
        context: GRPCAsyncServerCallContext
    ) async throws -> Protocol_MetadataResponse {
        var resp = Protocol_MetadataResponse()
        resp.name = pluginName
        resp.version = pluginVersion
        resp.qntxVersion = ">= 0.1.0"
        resp.description = "GPU-accelerated data visualization via Metal compute and render pipelines"
        resp.author = "QNTX"
        resp.license = "MIT"
        return resp
    }

    // MARK: - Initialize

    func initialize(
        request: Protocol_InitializeRequest,
        context: GRPCAsyncServerCallContext
    ) async throws -> Protocol_InitializeResponse {
        self.atsEndpoint = request.atsStoreEndpoint
        self.authToken = request.authToken
        self.config = request.config

        try renderer.setup()

        var resp = Protocol_InitializeResponse()
        return resp
    }

    // MARK: - Shutdown

    func shutdown(
        request: Protocol_Empty,
        context: GRPCAsyncServerCallContext
    ) async throws -> Protocol_Empty {
        renderer.teardown()
        return Protocol_Empty()
    }

    // MARK: - Health

    func health(
        request: Protocol_Empty,
        context: GRPCAsyncServerCallContext
    ) async throws -> Protocol_HealthResponse {
        var resp = Protocol_HealthResponse()
        resp.healthy = renderer.isReady
        resp.message = renderer.isReady ? "Metal device active" : "Metal device not available"
        if renderer.isReady {
            resp.details["device"] = renderer.deviceName
        }
        return resp
    }

    // MARK: - ConfigSchema

    func configSchema(
        request: Protocol_Empty,
        context: GRPCAsyncServerCallContext
    ) async throws -> Protocol_ConfigSchemaResponse {
        var resp = Protocol_ConfigSchemaResponse()
        // No required config fields yet — Metal device is auto-detected
        return resp
    }

    // MARK: - RegisterGlyphs

    func registerGlyphs(
        request: Protocol_Empty,
        context: GRPCAsyncServerCallContext
    ) async throws -> Protocol_GlyphDefResponse {
        var resp = Protocol_GlyphDefResponse()

        var vizGlyph = Protocol_GlyphDef()
        vizGlyph.symbol = "\u{25C8}" // ◈
        vizGlyph.title = "Metal Visualizer"
        vizGlyph.label = "swift-metal"
        vizGlyph.modulePath = "/viz-module.js"
        vizGlyph.defaultWidth = 800
        vizGlyph.defaultHeight = 600
        resp.glyphs = [vizGlyph]

        return resp
    }

    // MARK: - HandleHTTP

    func handleHTTP(
        request: Protocol_HTTPRequest,
        context: GRPCAsyncServerCallContext
    ) async throws -> Protocol_HTTPResponse {
        let path = request.path
        let method = request.method

        if method == "GET" && path == "/viz-module.js" {
            return serveGlyphModule()
        }

        if method == "POST" && path == "/render" {
            return await handleRender(request)
        }

        if method == "GET" && path == "/status" {
            return handleStatus()
        }

        var resp = Protocol_HTTPResponse()
        resp.statusCode = 404
        resp.body = Data("{\"error\":\"not found: \(path)\"}".utf8)
        resp.headers = [jsonHeader()]
        return resp
    }

    // MARK: - HandleWebSocket (stub)

    func handleWebSocket(
        requestStream: GRPCAsyncRequestStream<Protocol_WebSocketMessage>,
        responseStream: GRPCAsyncResponseStreamWriter<Protocol_WebSocketMessage>,
        context: GRPCAsyncServerCallContext
    ) async throws {
        // WebSocket streaming for live render updates — not yet implemented
    }

    // MARK: - ExecuteJob (stub)

    func executeJob(
        request: Protocol_ExecuteJobRequest,
        context: GRPCAsyncServerCallContext
    ) async throws -> Protocol_ExecuteJobResponse {
        var resp = Protocol_ExecuteJobResponse()
        resp.success = false
        resp.error = "swift-metal does not handle async jobs"
        resp.pluginVersion = pluginVersion
        return resp
    }

    // MARK: - ParseAxQuery (stub)

    func parseAxQuery(
        request: Protocol_ParseAxQueryRequest,
        context: GRPCAsyncServerCallContext
    ) async throws -> Protocol_ParseAxQueryResponse {
        var resp = Protocol_ParseAxQueryResponse()
        resp.error = "swift-metal does not parse Ax queries"
        return resp
    }

    // MARK: - HTTP Handlers

    private func serveGlyphModule() -> Protocol_HTTPResponse {
        var resp = Protocol_HTTPResponse()
        resp.statusCode = 200
        resp.body = Data(glyphModuleSource.utf8)
        resp.headers = [jsHeader()]
        return resp
    }

    private func handleRender(_ request: Protocol_HTTPRequest) async -> Protocol_HTTPResponse {
        // Accepts JSON with visualization data, renders via Metal, returns PNG
        var resp = Protocol_HTTPResponse()

        guard renderer.isReady else {
            resp.statusCode = 503
            resp.body = Data("{\"error\":\"Metal device not available\"}".utf8)
            resp.headers = [jsonHeader()]
            return resp
        }

        guard let pngData = renderer.renderToImage(data: request.body, width: 800, height: 600) else {
            resp.statusCode = 500
            resp.body = Data("{\"error\":\"render failed\"}".utf8)
            resp.headers = [jsonHeader()]
            return resp
        }

        resp.statusCode = 200
        resp.body = pngData
        var contentType = Protocol_HTTPHeader()
        contentType.name = "Content-Type"
        contentType.values = ["image/png"]
        resp.headers = [contentType]
        return resp
    }

    private func handleStatus() -> Protocol_HTTPResponse {
        var resp = Protocol_HTTPResponse()
        resp.statusCode = 200
        let status = """
        {"name":"\(pluginName)","version":"\(pluginVersion)","metal_ready":\(renderer.isReady),"device":"\(renderer.deviceName)"}
        """
        resp.body = Data(status.utf8)
        resp.headers = [jsonHeader()]
        return resp
    }

    // MARK: - Helpers

    private func jsonHeader() -> Protocol_HTTPHeader {
        var h = Protocol_HTTPHeader()
        h.name = "Content-Type"
        h.values = ["application/json"]
        return h
    }

    private func jsHeader() -> Protocol_HTTPHeader {
        var h = Protocol_HTTPHeader()
        h.name = "Content-Type"
        h.values = ["application/javascript"]
        return h
    }
}
