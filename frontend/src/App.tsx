import { AppHeader } from "@/components/layout/app-header";
import { AppSidebar } from "@/components/layout/app-sidebar";
import { StatusBar } from "@/components/layout/status-bar";
import { ConnectionsView } from "@/components/connections/connections-view";
import { LogsView } from "@/components/logs/logs-view";
import { InterceptPanel } from "@/components/network/intercept-panel";
import { NetworkView } from "@/components/network/network-view";
import { PortForwardPanel } from "@/components/network/portfwd-panel";
import { PreviewPanel } from "@/components/network/preview-panel";
import { OverviewView } from "@/components/overview/overview-view";
import { SettingsView } from "@/components/settings/settings-view";
import { PageShell } from "@/components/shared/page-shell";
import { useSession } from "@/hooks/use-session";
import { useI18n } from "@/i18n";

function App() {
  const { t } = useI18n();
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
          {view === "network" && (
            <NetworkView discovery={discovery} ready={ready} />
          )}
          {view === "network-portfwd" && (
            <PageShell
              title={t("portfwd.title")}
              description={t("portfwd.description")}
            >
              <PortForwardPanel
                embedded
                contextName={contextName}
                namespace={namespace}
              />
            </PageShell>
          )}
          {view === "network-exchange" && (
            <PageShell
              title={t("intercept.title")}
              description={t("intercept.description")}
            >
              <InterceptPanel
                embedded
                ready={ready}
                contextName={contextName}
                namespace={namespace}
              />
            </PageShell>
          )}
          {view === "network-preview" && (
            <PageShell
              title={t("preview.title")}
              description={t("preview.description")}
            >
              <PreviewPanel embedded ready={ready} namespace={namespace} />
            </PageShell>
          )}
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

        <StatusBar
          phase={session.phase}
          clusterName={currentContext?.cluster || session.context}
          contextName={session.context || contextName}
          namespace={session.namespace || namespace}
          message={uiError || session.error || session.message}
        />
      </div>
    </div>
  );
}

export default App;
