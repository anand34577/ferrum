import type { ComponentType } from "react"
import type { WidgetSettings, WidgetType } from "@/lib/dashboardTypes"
import { AlertActivityWidget } from "./widgets/AlertActivityWidget"
import { BackupActivityWidget } from "./widgets/BackupActivityWidget"
import { CapacityPlanningWidget } from "./widgets/CapacityPlanningWidget"
import { ClusterComparisonWidget } from "./widgets/ClusterComparisonWidget"
import { ConnectionStatusWidget } from "./widgets/ConnectionStatusWidget"
import { CpuByNodeWidget } from "./widgets/CpuByNodeWidget"
import { FleetOverviewWidget } from "./widgets/FleetOverviewWidget"
import { FleetSummaryWidget } from "./widgets/FleetSummaryWidget"
import { FleetTrendWidget } from "./widgets/FleetTrendWidget"
import { GuestStatusWidget } from "./widgets/GuestStatusWidget"
import { MemoryByNodeWidget } from "./widgets/MemoryByNodeWidget"
import { NodeCompositionWidget } from "./widgets/NodeCompositionWidget"
import { NodeComparisonWidget } from "./widgets/NodeComparisonWidget"
import { NodeScatterWidget } from "./widgets/NodeScatterWidget"
import { RunningTasksWidget } from "./widgets/RunningTasksWidget"
import { StorageTreemapWidget } from "./widgets/StorageTreemapWidget"
import { StorageUsageWidget } from "./widgets/StorageUsageWidget"
import { TopConsumersWidget } from "./widgets/TopConsumersWidget"
import { UtilizationHeatmapWidget } from "./widgets/UtilizationHeatmapWidget"
import { UtilizationHistogramWidget } from "./widgets/UtilizationHistogramWidget"
import { UptimeLeaderboardWidget } from "./widgets/UptimeLeaderboardWidget"

export const widgetRegistry: Record<WidgetType, ComponentType<{ settings: WidgetSettings }>> = {
  "fleet-overview": FleetOverviewWidget,
  "cluster-comparison": ClusterComparisonWidget,
  "fleet-summary": FleetSummaryWidget,
  "connection-status": ConnectionStatusWidget,
  "top-consumers": TopConsumersWidget,
  "running-tasks": RunningTasksWidget,
  "storage-usage": StorageUsageWidget,
  "storage-treemap": StorageTreemapWidget,
  "guest-status": GuestStatusWidget,
  "guest-histogram": UtilizationHistogramWidget,
  "utilization-heatmap": UtilizationHeatmapWidget,
  "cpu-by-node": CpuByNodeWidget,
  "memory-by-node": MemoryByNodeWidget,
  "capacity-planning": CapacityPlanningWidget,
  "node-scatter": NodeScatterWidget,
  "fleet-trend": FleetTrendWidget,
  "node-comparison": NodeComparisonWidget,
  "node-composition": NodeCompositionWidget,
  "alert-activity": AlertActivityWidget,
  "backup-activity": BackupActivityWidget,
  "uptime-leaderboard": UptimeLeaderboardWidget,
}
