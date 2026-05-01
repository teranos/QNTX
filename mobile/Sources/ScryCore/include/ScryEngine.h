// Objective-C++ bridge over scry's InferenceEngine. Swift talks to this;
// this talks to the C++ class in qntx-plugins/scry/src/.
//
// The C++ types (TokenSignal, SamplerConfig, ChatResult) are wrapped as
// Objective-C classes so Swift sees them as native types. Generation runs
// on a background queue; the token block fires on that queue and the
// completion fires on the caller's queue.

#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

extern NSString *const ScryErrorDomain;

typedef NS_ERROR_ENUM(ScryErrorDomain, ScryError) {
    ScryErrorModelLoadFailed = 1,
    ScryErrorModelNotLoaded = 2,
    ScryErrorGenerationFailed = 3,
    ScryErrorForkFailed = 4,
};

@interface ScryTokenCandidate : NSObject
@property (nonatomic, readonly) int tokenId;
@property (nonatomic, readonly, copy) NSString *text;
@property (nonatomic, readonly) float probability;
- (instancetype)initWithTokenId:(int)tokenId
                            text:(NSString *)text
                     probability:(float)probability NS_DESIGNATED_INITIALIZER;
- (instancetype)init NS_UNAVAILABLE;
@end

// Per-token signal captured before sampling. Mirrors C++ TokenSignal.
// `topK` is sorted high-to-low probability.
@interface ScryTokenSignal : NSObject
@property (nonatomic, readonly) int tokenId;
@property (nonatomic, readonly, copy) NSString *text;
@property (nonatomic, readonly) float confidence;   // P(chosen)
@property (nonatomic, readonly) float entropy;      // bits
@property (nonatomic, readonly) float topGap;       // P(top1) - P(top2)
@property (nonatomic, readonly, copy) NSArray<ScryTokenCandidate *> *topK;
// Absolute KV-cache position. Pass to forkFromResult:forkPosition:.
@property (nonatomic, readonly) int kvPosition;
@end

@interface ScryMessage : NSObject
@property (nonatomic, readonly, copy) NSString *role;     // "system" | "user" | "assistant"
@property (nonatomic, readonly, copy) NSString *content;
- (instancetype)initWithRole:(NSString *)role content:(NSString *)content NS_DESIGNATED_INITIALIZER;
- (instancetype)init NS_UNAVAILABLE;
@end

@interface ScryGenerationOptions : NSObject
@property (nonatomic) float temperature;
@property (nonatomic) int maxTokens;
@property (nonatomic) int topK;       // 0 = disabled
@property (nonatomic) float topP;     // 1.0 = disabled
@property (nonatomic) float minP;     // 0.0 = disabled
+ (instancetype)defaults;
@end

@interface ScryGenerationResult : NSObject
@property (nonatomic, readonly, copy) NSString *content;
@property (nonatomic, readonly) int promptTokens;
@property (nonatomic, readonly) int completionTokens;
@property (nonatomic, readonly, copy) NSArray<ScryTokenSignal *> *signals;
// llama.cpp seq_id this generation occupied — required for fork.
@property (nonatomic, readonly) int32_t sequenceId;
@end

// Return NO from the block to abort generation.
typedef BOOL (^ScryTokenHandler)(NSString *text, ScryTokenSignal *signal);

@interface ScryEngine : NSObject

- (instancetype)init NS_DESIGNATED_INITIALIZER;

// Load a GGUF model from disk. Blocks; call from a background queue.
- (BOOL)loadModelAtPath:(NSString *)path
            contextSize:(int)nCtx
                  error:(NSError * _Nullable * _Nullable)error;

- (void)unload;

@property (nonatomic, readonly) BOOL isLoaded;
@property (nonatomic, readonly, copy, nullable) NSString *modelName;
@property (nonatomic, readonly) int vocabSize;

- (nullable NSString *)textForTokenId:(int)tokenId;

// vocabSize × 6 floats: xyz position then rgb color, both PCA-derived.
// Returns NULL until the model is loaded and PCA has run.
// The pointer is stable until the next call to unload.
- (nullable const float *)vocabPositions3D NS_RETURNS_INNER_POINTER;

// Streaming generation. Token block fires on a background queue per token.
// Completion fires on the same queue once generation ends or aborts.
- (void)generateWithMessages:(NSArray<ScryMessage *> *)messages
                     options:(ScryGenerationOptions *)options
                onToken:(ScryTokenHandler)onToken
                  completion:(void (^)(ScryGenerationResult * _Nullable result,
                                       NSError * _Nullable error))completion;

// RDR: replay parent's KV cache up to forkPosition, decode forkTokenId on a
// new sequence at that position, then continue generation. forkPosition is
// the kvPosition of the token being replaced.
- (void)forkFromResult:(ScryGenerationResult *)parent
          forkPosition:(int)forkPosition
           forkTokenId:(int)forkTokenId
               options:(ScryGenerationOptions *)options
               onToken:(ScryTokenHandler)onToken
            completion:(void (^)(ScryGenerationResult * _Nullable result,
                                 NSError * _Nullable error))completion;

@end

NS_ASSUME_NONNULL_END
