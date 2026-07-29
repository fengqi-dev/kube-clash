import { AppHeader } from "@/components/layout/app-header";
import { AppSidebar } from "@/components/layout/app-sidebar";
import { ConnectionsView } from "@/components/connections/connections-view";
import { LogsView } from "@/components/logs/logs-view";
import { NetworkView } from "@/components/network/network-view";
import { OverviewView } from "@/components/overview/overview-view";
import { SettingsView } from "@/components/settings/settings-view";
import { useSession } from "@/hooks/use-session";

function App() {
  const {
    data,
    contextName,
    namespace,
    view,
    setView,
    loading,
    uiError,
    updateBusy,
    session,
    busy,
    ready,
    discovery,
    currentContext,
    setNamespace,
    changeContext,
    toggleConnection,
    checkForUpdates,
    openUpdatePage,
  } = useSession();

  return (
    <div className="flex h-screen min-h-[580px] overflow-hidden bg-background text-foreground">
      <AppSidebar
        view={view}
        ready={ready}
        coreVersion={session.coreVersion}
        updateAvailable={data.update.available}
        onNavigate={setView}
      />

      <div className="flex min-w-0 flex-1 flex-col">
        <AppHeader
          view={view}
          phase={session.phase}
          onOpenSettings={() => setView("settings")}
        />

        <main className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
          {view === "overview" && (
            <OverviewView
              contexts={data.contexts}
              namespaces={data.namespaces}
              contextName={contextName}
              namespace={namespace}
              clusterName={currentContext?.cluster ?? ""}
              session={session}
              discovery={discovery}
              loading={loading}
              error={uiError || session.error || ""}
              busy={busy}
              ready={ready}
              onContextChange={(value) => void changeContext(value)}
              onNamespaceChange={setNamespace}
              onToggle={() => void toggleConnection()}
            />
          )}
          {view === "connections" && (
            <ConnectionsView ready={ready} metrics={session.metrics} />
          )}
          {view === "network" && <NetworkView discovery={discovery} ready={ready} />}
          {view === "logs" && (
            <LogsView session={session} error={uiError || session.error || ""} />
          )}
          {view === "settings" && (
            <SettingsView
              update={data.update}
              checking={updateBusy}
              onCheck={() => void checkForUpdates()}
              onOpen={() => void openUpdatePage()}
            />
          )}
        </main>
      </div>
    </div>
  );
}

export default App;
