%%% @doc Process health monitor — gen_server that tracks supervision state.
%%%
%%% This is the seed. Right now it tracks:
%%%   - Uptime (how long the OTP application has been running)
%%%   - Supervised process count and restarts
%%%
%%% When the plugin manager is rewritten in Erlang, this becomes the
%%% health aggregator that collects state from per-plugin supervisors
%%% and reports to the QNTX server via gRPC callbacks.
-module(sentry_monitor).
-behaviour(gen_server).

-export([start_link/0, get_state/0]).
-export([init/1, handle_call/3, handle_cast/2, handle_info/2]).

-record(state, {
    started_at :: erlang:timestamp(),
    health_checks :: non_neg_integer()
}).

start_link() ->
    gen_server:start_link({local, ?MODULE}, ?MODULE, [], []).

%% @doc Returns current monitor state as a map (for health reporting).
get_state() ->
    gen_server:call(?MODULE, get_state).

init([]) ->
    {ok, #state{
        started_at = os:timestamp(),
        health_checks = 0
    }}.

handle_call(get_state, _From, State) ->
    Uptime = timer:now_diff(os:timestamp(), State#state.started_at) div 1_000_000,
    NewState = State#state{health_checks = State#state.health_checks + 1},
    Reply = #{
        uptime_secs => Uptime,
        health_checks => NewState#state.health_checks,
        supervised_processes => count_supervised(),
        otp_release => erlang:system_info(otp_release),
        process_count => erlang:system_info(process_count),
        memory_mb => erlang:memory(total) div (1024 * 1024)
    },
    {reply, Reply, NewState};
handle_call(_Request, _From, State) ->
    {reply, {error, unknown_request}, State}.

handle_cast(_Msg, State) ->
    {noreply, State}.

handle_info(_Info, State) ->
    {noreply, State}.

%% Count children of the sentry supervisor
count_supervised() ->
    case erlang:whereis(sentry_sup) of
        undefined -> 0;
        _Pid ->
            Props = supervisor:count_children(sentry_sup),
            proplists:get_value(active, Props, 0)
    end.
