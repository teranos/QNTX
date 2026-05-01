// Bridge implementation. Owns a single C++ InferenceEngine and serializes
// generation calls on a private dispatch queue. Token positions are
// reconstructed from prompt_tokens + token index — InferenceEngine itself
// uses sequence 0 for chat() and an explicit sequence for fork_and_generate.

#import "ScryEngine.h"

// Mobile build defines SCRY_NO_GRPC before including plugin.h so the gRPC
// service classes at the bottom of plugin.h are skipped.
#define SCRY_NO_GRPC
#include "plugin.h"

#include <memory>
#include <string>
#include <vector>

NSString *const ScryErrorDomain = @"com.qntx.scry";

// MARK: - Value types

@interface ScryTokenCandidate ()
@property (nonatomic) int tokenId;
@property (nonatomic, copy) NSString *text;
@property (nonatomic) float probability;
@end

@implementation ScryTokenCandidate
- (instancetype)initWithTokenId:(int)tokenId
                            text:(NSString *)text
                     probability:(float)probability {
    if ((self = [super init])) {
        _tokenId = tokenId;
        _text = [text copy];
        _probability = probability;
    }
    return self;
}
@end

@interface ScryTokenSignal ()
@property (nonatomic) int tokenId;
@property (nonatomic, copy) NSString *text;
@property (nonatomic) float confidence;
@property (nonatomic) float entropy;
@property (nonatomic) float topGap;
@property (nonatomic, copy) NSArray<ScryTokenCandidate *> *topK;
@property (nonatomic) int kvPosition;
@end

@implementation ScryTokenSignal
@end

@implementation ScryMessage
- (instancetype)initWithRole:(NSString *)role content:(NSString *)content {
    if ((self = [super init])) {
        _role = [role copy];
        _content = [content copy];
    }
    return self;
}
@end

@implementation ScryGenerationOptions
+ (instancetype)defaults {
    ScryGenerationOptions *o = [[self alloc] init];
    o.temperature = 0.7f;
    o.maxTokens = 256;
    o.topK = 40;
    o.topP = 0.95f;
    o.minP = 0.05f;
    return o;
}
@end

@interface ScryGenerationResult ()
@property (nonatomic, copy) NSString *content;
@property (nonatomic) int promptTokens;
@property (nonatomic) int completionTokens;
@property (nonatomic, copy) NSArray<ScryTokenSignal *> *signals;
@property (nonatomic) int32_t sequenceId;
@end

@implementation ScryGenerationResult
@end

// MARK: - Engine

@implementation ScryEngine {
    std::unique_ptr<InferenceEngine> _engine;
    dispatch_queue_t _workQueue;
    int32_t _nextSequenceId;  // 0 = root, allocated upward for forks
}

- (instancetype)init {
    if ((self = [super init])) {
        _engine = std::make_unique<InferenceEngine>();
        _workQueue = dispatch_queue_create("com.qntx.scry.inference", DISPATCH_QUEUE_SERIAL);
        _nextSequenceId = 1;  // 0 reserved for root chat
    }
    return self;
}

- (void)dealloc {
    if (_engine) {
        _engine->unload();
    }
}

- (BOOL)loadModelAtPath:(NSString *)path
            contextSize:(int)nCtx
                  error:(NSError **)error {
    std::string cppPath = [path UTF8String];
    bool ok = _engine->load_model(cppPath, nCtx);
    if (!ok) {
        if (error) {
            *error = [NSError errorWithDomain:ScryErrorDomain
                                         code:ScryErrorModelLoadFailed
                                     userInfo:@{NSLocalizedDescriptionKey:
                                                  [NSString stringWithFormat:@"failed to load model at %@", path]}];
        }
        return NO;
    }
    _nextSequenceId = 1;
    return YES;
}

- (void)unload {
    _engine->unload();
}

- (BOOL)isLoaded {
    return _engine->is_loaded();
}

- (NSString *)modelName {
    if (!_engine->is_loaded()) return nil;
    return [NSString stringWithUTF8String:_engine->model_name().c_str()];
}

- (int)vocabSize {
    return _engine->vocab_size();
}

- (NSString *)textForTokenId:(int)tokenId {
    std::string s = _engine->token_text(tokenId);
    if (s.empty()) return nil;
    return [NSString stringWithUTF8String:s.c_str()];
}

- (const float *)vocabPositions3D {
    if (!_engine->is_loaded()) return NULL;
    const std::vector<float> &positions = _engine->vocab_positions_3d();
    if (positions.empty()) return NULL;
    return positions.data();
}

// MARK: - Generation

static ScryTokenSignal *wrapSignal(const TokenSignal &sig, int kvPosition) {
    NSMutableArray<ScryTokenCandidate *> *cands = [NSMutableArray arrayWithCapacity:sig.top_k.size()];
    for (const TokenCandidate &c : sig.top_k) {
        ScryTokenCandidate *wc =
            [[ScryTokenCandidate alloc] initWithTokenId:c.id
                                                   text:[NSString stringWithUTF8String:c.text.c_str()]
                                            probability:c.prob];
        [cands addObject:wc];
    }

    ScryTokenSignal *out = [[ScryTokenSignal alloc] init];
    out.tokenId = sig.token_id;
    out.text = [NSString stringWithUTF8String:sig.token_text.c_str()];
    out.confidence = sig.confidence;
    out.entropy = sig.entropy;
    out.topGap = sig.top_gap;
    out.topK = cands;
    out.kvPosition = kvPosition;
    return out;
}

static SamplerConfig samplerFromOptions(ScryGenerationOptions *o) {
    SamplerConfig cfg;
    cfg.top_k = o.topK;
    cfg.top_p = o.topP;
    cfg.min_p = o.minP;
    return cfg;
}

- (void)generateWithMessages:(NSArray<ScryMessage *> *)messages
                     options:(ScryGenerationOptions *)options
                     onToken:(ScryTokenHandler)onToken
                  completion:(void (^)(ScryGenerationResult *, NSError *))completion {
    if (!_engine->is_loaded()) {
        completion(nil, [NSError errorWithDomain:ScryErrorDomain
                                            code:ScryErrorModelNotLoaded
                                        userInfo:@{NSLocalizedDescriptionKey: @"no model loaded"}]);
        return;
    }

    // Snapshot inputs for the worker block.
    std::vector<InferenceEngine::Message> cppMessages;
    cppMessages.reserve(messages.count);
    for (ScryMessage *m in messages) {
        InferenceEngine::Message cm;
        cm.role = [m.role UTF8String];
        cm.content = [m.content UTF8String];
        cppMessages.push_back(std::move(cm));
    }
    SamplerConfig cfg = samplerFromOptions(options);
    float temperature = options.temperature;
    int maxTokens = options.maxTokens;

    ScryTokenHandler tokenBlock = [onToken copy];

    dispatch_async(_workQueue, ^{
        // KV cache is cleared inside InferenceEngine::stream_chat — root chat
        // always uses sequence 0. Track positions ourselves.
        __block int chosenIndex = 0;
        __block int promptTokens = 0;
        __block BOOL aborted = NO;

        auto callback = [&](const std::string &text, const TokenSignal &sig) -> bool {
            // promptTokens not yet known at first call — InferenceEngine fills
            // result.prompt_tokens at the end. We use an estimated kv_position
            // here; the final, accurate position is set in the result signals
            // below. Most callers should read kvPosition from the result.
            int kvPos = promptTokens + chosenIndex;
            chosenIndex++;
            if (tokenBlock) {
                ScryTokenSignal *wrapped = wrapSignal(sig, kvPos);
                NSString *nsText = [NSString stringWithUTF8String:text.c_str()];
                BOOL keepGoing = tokenBlock(nsText, wrapped);
                if (!keepGoing) {
                    aborted = YES;
                    return false;
                }
            }
            return true;
        };

        InferenceEngine::ChatResult cppResult =
            _engine->stream_chat(cppMessages, temperature, maxTokens, callback, cfg);

        promptTokens = cppResult.prompt_tokens;

        // Build wrapped result with accurate kv positions.
        NSMutableArray<ScryTokenSignal *> *signals =
            [NSMutableArray arrayWithCapacity:cppResult.signals.size()];
        for (size_t i = 0; i < cppResult.signals.size(); i++) {
            int kvPos = cppResult.prompt_tokens + (int)i;
            [signals addObject:wrapSignal(cppResult.signals[i], kvPos)];
        }

        ScryGenerationResult *r = [[ScryGenerationResult alloc] init];
        r.content = [NSString stringWithUTF8String:cppResult.content.c_str()];
        r.promptTokens = cppResult.prompt_tokens;
        r.completionTokens = cppResult.completion_tokens;
        r.signals = signals;
        r.sequenceId = 0;  // root sequence

        if (aborted && cppResult.content.empty()) {
            completion(nil, [NSError errorWithDomain:ScryErrorDomain
                                                code:ScryErrorGenerationFailed
                                            userInfo:@{NSLocalizedDescriptionKey: @"aborted"}]);
            return;
        }
        completion(r, nil);
    });
}

- (void)forkFromResult:(ScryGenerationResult *)parent
          forkPosition:(int)forkPosition
           forkTokenId:(int)forkTokenId
               options:(ScryGenerationOptions *)options
               onToken:(ScryTokenHandler)onToken
            completion:(void (^)(ScryGenerationResult *, NSError *))completion {
    if (!_engine->is_loaded()) {
        completion(nil, [NSError errorWithDomain:ScryErrorDomain
                                            code:ScryErrorModelNotLoaded
                                        userInfo:@{NSLocalizedDescriptionKey: @"no model loaded"}]);
        return;
    }

    int32_t parentSeq = parent.sequenceId;
    int32_t newSeq = _nextSequenceId++;
    SamplerConfig cfg = samplerFromOptions(options);
    float temperature = options.temperature;
    int maxTokens = options.maxTokens;
    int promptTokens = parent.promptTokens;
    ScryTokenHandler tokenBlock = [onToken copy];

    dispatch_async(_workQueue, ^{
        __block int chosenIndex = 0;
        __block BOOL aborted = NO;

        auto callback = [&](const std::string &text, const TokenSignal &sig) -> bool {
            int kvPos = forkPosition + 1 + chosenIndex;
            chosenIndex++;
            if (tokenBlock) {
                ScryTokenSignal *wrapped = wrapSignal(sig, kvPos);
                NSString *nsText = [NSString stringWithUTF8String:text.c_str()];
                BOOL keepGoing = tokenBlock(nsText, wrapped);
                if (!keepGoing) {
                    aborted = YES;
                    return false;
                }
            }
            return true;
        };

        InferenceEngine::ChatResult cppResult = _engine->fork_and_generate(
            parentSeq, forkPosition, forkTokenId, newSeq,
            temperature, maxTokens, callback, cfg);

        // The fork token is the first token of the new branch at forkPosition.
        // Subsequent tokens occupy forkPosition + 1, +2, ... in the new seq.
        NSMutableArray<ScryTokenSignal *> *signals =
            [NSMutableArray arrayWithCapacity:cppResult.signals.size()];
        for (size_t i = 0; i < cppResult.signals.size(); i++) {
            int kvPos = forkPosition + 1 + (int)i;
            [signals addObject:wrapSignal(cppResult.signals[i], kvPos)];
        }

        ScryGenerationResult *r = [[ScryGenerationResult alloc] init];
        r.content = [NSString stringWithUTF8String:cppResult.content.c_str()];
        r.promptTokens = promptTokens;
        r.completionTokens = cppResult.completion_tokens;
        r.signals = signals;
        r.sequenceId = newSeq;

        if (aborted && cppResult.content.empty()) {
            completion(nil, [NSError errorWithDomain:ScryErrorDomain
                                                code:ScryErrorForkFailed
                                            userInfo:@{NSLocalizedDescriptionKey: @"aborted"}]);
            return;
        }
        completion(r, nil);
    });
}

@end
