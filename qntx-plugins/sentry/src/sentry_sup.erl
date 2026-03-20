%%% @doc Top-level OTP supervisor for the sentry plugin.
%%%
%%% one_for_one strategy: if the monitor crashes, restart it independently.
%%% This is the seed of the supervision tree that will grow to manage
%%% plugin processes when the plugin manager is rewritten in Erlang.
%%%
%%% Future children:
%%%   - Per-plugin supervisor (one_for_one, manages a single plugin lifecycle)
%%%   - Health aggregator (collects health from all plugin supervisors)
%%%   - Connection pool supervisor (manages gRPC connections to plugins)
-module(sentry_sup).
-behaviour(supervisor).
-export([start_link/0, init/1]).

start_link() ->
    supervisor:start_link({local, ?MODULE}, ?MODULE, []).

init([]) ->
    SupFlags = #{
        strategy => one_for_one,
        intensity => 5,     %% Max 5 restarts
        period => 60        %% Within 60 seconds
    },

    Monitor = #{
        id => sentry_monitor,
        start => {sentry_monitor, start_link, []},
        restart => permanent,
        shutdown => 5000,
        type => worker,
        modules => [sentry_monitor]
    },

    {ok, {SupFlags, [Monitor]}}.
