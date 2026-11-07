CC ?= cc
AR ?= ar
PKG_CONFIG ?= pkg-config
PREFIX ?= /usr/local
BUILD ?= build
CPPFLAGS += -Iinclude -I$(BUILD) $(shell $(PKG_CONFIG) --cflags json-c)
# 「[Opus 5.5] The protocol's own list of its methods, as the release it is held to ships it.」
META = schema/v1/meta.json
CFLAGS ?= -O2 -g
CFLAGS += -std=c11 -Wall -Wextra -Wpedantic -Werror -fPIC
# 「[Opus 5.5] Every implicit narrowing, shadowed name, unprototyped function and non-literal format is an error.」
CFLAGS += -Wshadow -Wconversion -Wsign-conversion -Wstrict-prototypes -Wmissing-prototypes \
	-Wcast-qual -Wformat=2 -Wundef -Wvla -Wnull-dereference -Wdouble-promotion -Wimplicit-fallthrough
SANITIZE = -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer
LDLIBS += $(shell $(PKG_CONFIG) --libs json-c)
UNAME := $(shell uname -s)
ifeq ($(UNAME),Darwin)
SHARED = $(BUILD)/libacp.dylib
SHARED_FLAGS = -dynamiclib -Wl,-install_name,@rpath/libacp.dylib
else
SHARED = $(BUILD)/libacp.so
SHARED_FLAGS = -shared -Wl,-soname,libacp.so
endif

.PHONY: all test install clean
all: $(BUILD)/libacp.a $(SHARED)

$(BUILD):
	mkdir -p $@

OBJECTS = $(addprefix $(BUILD)/,acp.o initialize.o session.o modes.o config.o prompt.o permission.o fs.o terminal.o updates.o client.o)

# 「[Opus 5.5] Every method name libacp writes or reads, generated from META, as the SDK generates its tables.」
$(BUILD)/methods: tools/methods.c | $(BUILD)
	$(CC) $(CPPFLAGS) $(CFLAGS) $(LDFLAGS) $< $(LDLIBS) -o $@

$(BUILD)/acp_methods.h: $(META) $(BUILD)/methods
	$(BUILD)/methods $(META) $@

$(BUILD)/%.o: src/%.c src/internal.h include/acp.h $(BUILD)/acp_methods.h | $(BUILD)
	$(CC) $(CPPFLAGS) $(CFLAGS) -c $< -o $@

$(BUILD)/libacp.a: $(OBJECTS)
	$(AR) rcs $@ $^

$(SHARED): $(OBJECTS)
	$(CC) $(CFLAGS) $(LDFLAGS) $(SHARED_FLAGS) -o $@ $^ $(LDLIBS)

TESTS = methods initialize session modes config prompt permission fs terminal updates client

test: all
	for t in $(TESTS); do \
		$(CC) $(CPPFLAGS) $(CFLAGS) $(LDFLAGS) tests/$$t.c $(BUILD)/libacp.a $(LDLIBS) -o $(BUILD)/$$t-test && $(BUILD)/$$t-test || exit 1; \
	done
# 「[Opus 5.5] The same tests built with the library's sources under AddressSanitizer and UndefinedBehaviorSanitizer.」
	for t in $(TESTS); do \
		$(CC) $(CPPFLAGS) $(CFLAGS) $(SANITIZE) $(LDFLAGS) tests/$$t.c $(addprefix src/,$(notdir $(OBJECTS:.o=.c))) $(LDLIBS) -o $(BUILD)/$$t-sanitized && $(BUILD)/$$t-sanitized || exit 1; \
	done

install: all
	install -d $(DESTDIR)$(PREFIX)/include $(DESTDIR)$(PREFIX)/lib/pkgconfig
	install -m 644 include/acp.h $(BUILD)/acp_methods.h $(DESTDIR)$(PREFIX)/include/
# 「[Opus 5.5] The archive alone, as libmcp installs: -lacp resolves to it with nothing to find at run time.」
	install -m 644 $(BUILD)/libacp.a $(DESTDIR)$(PREFIX)/lib/
	sed -e 's|@PREFIX@|$(PREFIX)|g' libacp.pc.in > $(BUILD)/libacp.pc
	install -m 644 $(BUILD)/libacp.pc $(DESTDIR)$(PREFIX)/lib/pkgconfig/

clean:
	rm -rf $(BUILD)
