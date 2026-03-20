%%% @doc OTP application behaviour for sentry.
%%%
%%% Starts the supervision tree. The tree is intentionally minimal:
%%% one supervisor, one monitor gen_server. This is the seed — future
%%% plugin supervision processes will be added as children here.
-module(sentry_app).
-behaviour(application).
-export([start/2, stop/1]).

start(_StartType, _StartArgs) ->
    sentry_sup:start_link().

stop(_State) ->
    ok.
