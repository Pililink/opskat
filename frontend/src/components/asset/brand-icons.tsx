// Re-export lucide-react icons as brand icon replacements.
// Clean stroke icons instead of monochrome brand logos — visually consistent
// and recognizable at small sizes without needing official logos.

import {
  CloudCog,
  CloudSun,
  CloudLightning,
  CloudMoon,
  CloudRain,
  CloudSnow,
  DatabaseZap,
  DatabaseBackup,
  Zap,
  Leaf,
  SearchCode,
  Box,
  Boxes,
  Terminal,
  AppWindow,
} from "lucide-react";

// ===== Cloud Providers =====
export const AwsIcon = CloudCog;
export const AzureIcon = CloudSun;
export const GcpIcon = CloudLightning;
export const AliCloudIcon = CloudMoon;
export const TencentCloudIcon = CloudRain;
export const HuaweiCloudIcon = CloudSnow;

// ===== Databases =====
export const MysqlIcon = DatabaseZap;
export const PostgresqlIcon = DatabaseBackup;
export const RedisIcon = Zap;
export const MongodbIcon = Leaf;
export const ElasticsearchIcon = SearchCode;

// ===== System / Platform =====
export const DockerIcon = Box;
export const KubernetesIcon = Boxes;
export const LinuxIcon = Terminal;
export const WindowsIcon = AppWindow;
