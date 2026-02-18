# 网络运维监控平台设计方案

**文档版本**：v1.38  
**编制日期**：2025-07-18  
**编制人**：霜叶飞  

---

## 一、需求概述

### 1.1 项目背景

随着企业IT基础设施规模不断扩大，网络设备、服务器、应用系统的数量和复杂度持续增长。传统的被动式运维模式已无法满足需求，亟需构建一套具备自动发现、实时监控、资产管理、告警通知、可视化展示能力的综合性网络运维监控平台。

### 1.2 核心目标

| 目标 | 描述 |
|------|------|
| **全栈发现** | 自动发现网络中的设备、服务器、应用，跨网段整合 |
| **资产管理** | 完整记录物理信息、配置信息、软件信息，支持多网卡/IP |
| **实时监控** | 通过SNMP/Agent采集状态数据，实时掌握运行状况 |
| **智能告警** | 多渠道通知（钉钉/企业微信/邮件/语音），支持阈值触发 |
| **可视化** | 机柜图、拓扑图、历史趋势图，现代GUI界面 |

---

## 二、需求详细分析

### 2.1 自动发现模块

#### 2.1.1 发现来源

| 来源类型 | 说明 |
|----------|------|
| **SNMP扫描** | 通过SNMP协议扫描网段内的设备（路由器、交换机、服务器） |
| **Ping探测** | ICMP探测存活主机 |
| **ARP扫描** | 获取局域网内MAC地址和IP对应关系 |
| **SSH/Agent** | 通过SSH或Agent获取更详细的资产信息 |
| **API导入** | 第三方系统API导入（VMware、OpenStack、云平台） |

#### 2.1.2 跨网段整合

```
┌─────────────────────────────────────────────────────────────┐
│                     发现任务调度中心                         │
├─────────────────────────────────────────────────────────────┤
│  发现任务A (192.168.1.0/24)  ─┐                            │
│  发现任务B (10.0.0.0/8)       │──> 统一归一化处理 ──> 资产库 │
│  发现任务C (172.16.0.0/12)  ─┘                            │
└─────────────────────────────────────────────────────────────┘
```

- **多任务并行**：不同网段可并行发起发现任务
- **统一归一化**：所有来源的数据统一格式、去重、关联
- **增量更新**：发现结果与已有资产比对，自动识别新增/变更/下线

#### 2.1.3 SNMP通讯字发现过程中合规检查

自动，系统会对 SNMP 通讯字进行安全合规性检查。

##### 合规标准

| 等级 | 通讯字类型 | 处理方式 |
|------|------------|----------|
| 🔴 **不合规** | public、private、manager、default、admin、root、12345 等常见默认字符串 | 记录并告警，要求整改 |
| 🟡 **警告** | 短于8位、纯数字、纯字母 | 建议修改，但不强制 |
| 🟢 **合规** | 复杂组合（大小写+数字+特殊字符，长度≥12位） | 正常通过 |

##### 内置默认通讯字黑名单

```
public, private, manager, default, admin, root, cisco, huawei, h3c, 
12345, 123456, password, letmein, welcome, octet, system, secret,
test, guest, operator, security, administrator, monitor, network
```

##### 合规检查流程

```
发现任务执行
     │
     ▼
遍历网段IP ──► SNMP探测（尝试通讯字列表）
     │
     ▼
  通讯成功？
     │
   ┌─┴─┐
   │   │
   是  否
   │   │
   ▼   ▼
检查通讯字 ◄────────── 下一IP
是否合规
   │
 ┌─┴─┐
 │   │
合规 不合规
 │   │
 ▼   ▼
标记"合规" 标记"不合规" + 生成整改工单
```

##### 不合规通讯字处理

1. **记录详情**
   - 设备IP、端口、SNMP版本
   - 使用的通讯字（加密存储）
   - 尝试时间、关联资产

2. **告警通知**
   - 立即通知安全管理员
   - 告警级别：warning（警告）

3. **整改工单**
   - 自动生成整改工单
   - 指派给网络/安全团队
   - 跟踪整改进度

4. **定期检查**
   - 每周扫描通讯字变更
   - 新增设备自动检查
   - 资产变更时复查

##### 数据库设计

```sql
-- 通讯字合规记录表
CREATE TABLE snmp_credential_compliance (
    id                  UUID PRIMARY KEY,
    asset_id            UUID REFERENCES assets(id),
    snmp_device_id      UUID REFERENCES snmp_devices(id),
    
    community_string    VARCHAR(100),           -- 加密存储
    is_compliant        BOOLEAN DEFAULT FALSE, -- 是否合规
    compliance_level    VARCHAR(20),            -- compliant/warning/non_compliant
    check_result        TEXT,                  -- 检查详情
    
    discovered_at       TIMESTAMP,              -- 发现时间
    last_checked_at    TIMESTAMP,              -- 最近检查时间
    
    ticket_id          UUID,                   -- 关联整改工单
    is_rectified       BOOLEAN DEFAULT FALSE,  -- 是否已整改
    
    created_at          TIMESTAMP,
    updated_at          TIMESTAMP
);
```

##### 管理界面

- **仪表盘**：显示不合规通讯字统计
- **资产详情**：显示每个设备的通讯字合规状态
- **整改工单**：跟踪整改进度
- **设置**：自定义黑名单、告警规则

---

### 2.2 SolarWinds Orion 9迁移模块

#### 2.2.1 迁移概述

对于从SolarWinds Orion 9系统迁移的场景，本平台提供完整的数据导出、配置转换和资产合并能力。

```
┌─────────────────────────────────────────────────────────────────────┐
│                   SolarWinds迁移架构                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────┐      ┌─────────────────┐                     │
│   │ SolarWinds      │      │  新监控平台     │                     │
│   │ Orion DB        │      │                 │                     │
│   │ (SQL Server)   │ ───► │  ┌───────────┐  │                     │
│   └─────────────────┘      │  │ 数据导出器 │  │                     │
│                           │  └─────┬─────┘  │                     │
│                           │        │         │                     │
│                           │        ▼         │                     │
│                           │  ┌───────────┐  │                     │
│   ┌─────────────────┐     │  │ 配置转换器 │  │                     │
│   │ 已有自动发现    │ ───► │  └─────┬─────┘  │                     │
│   │ 资产库          │     │        │         │                     │
│   └─────────────────┘     │        ▼         │                     │
│                           │  ┌───────────┐  │                     │
│                           │  │ 资产合并器 │──┼──► 统一资产库      │
│                           │  └───────────┘  │                     │
│                           └─────────────────┘                     │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

#### 2.2.2 数据导出方式

| 方式 | 说明 | 适用场景 |
|------|------|----------|
| **数据库直连** | 直接连接SolarWinds SQL Server导出 | 有数据库访问权限 |
| **API导出** | 通过SolarWinds REST API导出 | 只读API访问 |
| **CSV导出** | 手动导出CSV再导入 | 小规模迁移 |
| **Orion SDK** | 使用SolarWinds SDK工具导出 | 批量自动化 |

#### 2.2.3 导出数据范围

| 数据类型 | SolarWinds表/API | 映射到本平台 |
|----------|------------------|--------------|
| **节点信息** | `Nodes`表/API | 资产基本信息 |
| **接口信息** | `Interfaces`表/API | 网络接口 |
| **监控指标** | `Statistics`表/API | 监控配置 |
| **告警定义** | `AlertDefinitions`表/API | 告警规则 |
| **告警历史** | `AlertHistory`表/API | 告警历史 |
| **自定义属性** | `NodeCustomProperties`表 | 自定义字段 |
| **依赖关系** | `Dependencies`表/API | 拓扑关联 |
| **性能数据** | `Performance`表/API | 历史监控数据 |

#### 2.2.4 导出配置示例

```yaml
# solarwinds-migration-config.yaml
# SolarWinds数据导出配置

solarwinds:
  # 数据库连接 (直连方式)
  database:
    host: "solarwinds.company.com"
    port: 1433
    database: "SolarWindsOrion"
    username: "readonly_user"
    password: "${SOLARWINDS_DB_PASSWORD}"
    # 或使用Windows集成认证
    use_windows_auth: false
    
  # API方式 (推荐)
  api:
    base_url: "https://solarwinds.company.com:17778/SolarWinds"
    username: "api_readonly"
    password: "${SOLARWINDS_API_PASSWORD}"
    
  # 导出范围
  export:
    # 时间范围 (性能数据)
    performance:
      enabled: true
      start_date: "2024-01-01"
      end_date: "2025-07-18"
      # 保留精度
      aggregation: "hourly"  # raw/hourly/daily
    
    # 告警历史
    alert_history:
      enabled: true
      start_date: "2024-01-01"
    
    # 节点过滤
    node_filter:
      include: ["*"]  # 或指定列表
      exclude: ["test-.*", "dev-.*"]
      
# 数据映射配置
mapping:
  # 资产字段映射
  asset_fields:
    solarwinds_field: new_platform_field
    "Caption": "asset_name"
    "IP_Address": "primary_ip"
    "Description": "description"
    "Vendor": "brand"
    "Machine_Type": "model"
    "SNMPsysName": "sys_name"
    "SNMPsysLocation": "location"
    "SNMPsysContact": "contact"
    
  # 自定义属性映射
  custom_properties:
    # SolarWinds自定义属性 -> 本平台标签
    "Building": "idc_name"
    "Floor": "rack_position"
    "Support_Contract": "warranty_end"
    
  # 告警映射
  alert_mapping:
    # 告警级别映射
    "Critical": "critical"
    "Warning": "warning"
    "Information": "info"
    
    # 触发条件映射
    "Trigger": "trigger"
    "Reset": "reset"
    
# 合并策略
merge_strategy:
  # IP地址匹配
  match_by_ip: true
  
  # 主机名匹配
  match_by_hostname: true
  
  # MAC地址匹配
  match_by_mac: true
  
  # 匹配策略
  on_match: "update"  # update/skip/duplicate
  
  # 未匹配处理
  on_unmatched: "import"  # import/skip
  
  # 保留原监控配置
  preserve_monitoring: true
  
  # 保留原告警规则
  preserve_alerts: true

# 输出配置
output:
  # 导出格式
  format: "json"  # json/csv/database
  
  # 目标数据库
  target_database:
    host: "new-platform.company.com"
    port: 5432
    database: "monitor"
    
  # 预处理脚本
  pre_process: "./scripts/pre-process.py"
  post_process: "./scripts/post-process.py"
```

#### 2.2.5 数据库直连导出SQL

```sql
-- SolarWinds数据库导出脚本
-- 1. 导出节点信息
SELECT 
    n.NodeID,
    n.Caption,
    n.IP_Address,
    n.IP_Address_Type,
    n.SubType,
    n.MachineType,
    n.Vendor,
    n.SNMPVersion,
    n.SNMPv2_MIBs,
    n.SNMPsysName,
    n.SNMPsysLocation,
    n.SNMPsysContact,
    n.Status,
    n.UnManaged,
    n.Caption AS DisplayName,
    n.Description,
    n.LastSystemUpTimePollUtc,
    n.LastUpdated,
    n.Created
    
FROM Nodes n
WHERE n.Status != 9  -- 排除已禁用
  AND n.IP_Address LIKE '10.%'  -- 可选：按IP过滤;

-- 2. 导出接口信息
SELECT 
    i.InterfaceID,
    i.NodeID,
    i.Caption,
    i.InterfaceName,
    i.InterfaceType,
    i.InterfaceSubType,
    i.IP_Address,
    i.MAC_Address,
    i.ifSpeed,
    i.ifOperStatus,
    i.ifAdminStatus,
    i.ifHighSpeed,
    i.InBandwidth,
    i.OutBandwidth,
    i.InAveragebps,
    i.OutAveragebps,
    i.InterfaceIndex
    
FROM Interfaces i
WHERE i.NodeID IN (SELECT NodeID FROM Nodes WHERE Status != 9);

-- 3. 导出告警定义
SELECT 
    ad.AlertID,
    ad.Name,
    ad.Description,
    ad.TriggerCondition,
    ad.TriggerConditionType,
    ad.TriggerSeverity,
    ad.TriggerResetCondition,
    ad.TriggerResetSeverity,
    ad.Enabled,
    ad.AnnounceOnClear,
    ad.DeleteAfterDays,
    ad.Created,
    ad.LastModified
    
FROM AlertDefinitions ad
WHERE ad.Enabled = 1;

-- 4. 导出告警动作
SELECT 
    aa.AlertActionID,
    aa.AlertID,
    aa.ActionType,
    aa.ActionOrder,
    aa.AlertMessage,
    aa.ActionSettings,
    aa.Delay,
    aa.Trigger
    
FROM AlertActions aa
WHERE aa.AlertID IN (SELECT AlertID FROM AlertDefinitions WHERE Enabled = 1);

-- 5. 导出节点自定义属性
SELECT 
    ncp.NodeID,
    cp.PropertyName,
    cp.PropertyValue
    
FROM NodeCustomProperties ncp
JOIN CustomProperties cp ON ncp.PropertyID = cp.PropertyID
WHERE ncp.NodeID IN (SELECT NodeID FROM Nodes WHERE Status != 9);

-- 6. 导出监控指标配置
SELECT 
    msi.StatisticsID,
    msi.NodeID,
    msi.StatisticName,
    msi.StatisticDescription,
    msi.Units,
    msi.IsBoolean,
    msi.MinValue,
    msi.MaxValue
    
FROM ManagedEntityStatisticsInstances msi
WHERE msi.NodeID IN (SELECT NodeID FROM Nodes WHERE Status != 9);
```

#### 2.2.6 API导出方式

```bash
#!/bin/bash
# SolarWinds API导出脚本

# 配置
API_URL="https://solarwinds.company.com:17778/SolarWinds"
USERNAME="api_readonly"
PASSWORD="${SOLARWINDS_API_PASSWORD}"

# 获取认证Token
TOKEN=$(curl -s -k -X POST "${API_URL}/ Orion/LoginService/Discover" \
  -H "Content-Type: application/json" \
  -d "{\"Username\":\"${USERNAME}\",\"Password\":\"${PASSWORD}\"}" | jq -r '.Token')

# 1. 导出节点
echo "导出节点..."
curl -s -k -X GET "${API_URL}/Orion/Nodes" \
  -H "Authorization: Bearer ${TOKEN}" \
  -o nodes.json

# 2. 导出接口
echo "导出接口..."
curl -s -k -X GET "${API_URL}/Orion/Interfaces" \
  -H "Authorization: Bearer ${TOKEN}" \
  -o interfaces.json

# 3. 导出告警
echo "导出告警..."
curl -s -k -X GET "${API_URL}/Orion/AlertDescriptions" \
  -H "Authorization: Bearer ${TOKEN}" \
  -o alerts.json

# 4. 导出自定义属性
echo "导出自定义属性..."
curl -s -k -X GET "${API_URL}/Orion/NodeProperties" \
  -H "Authorization: Bearer ${TOKEN}" \
  -o node_properties.json

# 5. 导出依赖关系
echo "导出拓扑依赖..."
curl -s -k -X GET "${API_URL}/Orion/Dependencies" \
  -H "Authorization: Bearer ${TOKEN}" \
  -o dependencies.json

echo "导出完成!"
```

#### 2.2.7 配置转换器设计

```go
// SolarWinds配置转换器
type SolarWindsConverter struct {
    mappingConfig *MappingConfig
    logger        *Logger
}

// 节点转换
func (c *SolarWindsConverter) ConvertNode(swNode SolarWindsNode) *Asset {
    asset := &Asset{
        // 基础字段映射
        AssetName:   swNode.Caption,
        AssetTag:    fmt.Sprintf("SW-%d", swNode.NodeID),
        PrimaryIP:   swNode.IPAddress,
        Description: swNode.Description,
        Brand:       swNode.Vendor,
        Model:       swNode.MachineType,
        SN:          swNode.SerialNumber,
        
        // 位置信息
        Location:    swNode.SNMPsysLocation,
        Contact:     swNode.SNMPsysContact,
        
        // 状态
        Status:     c.mapStatus(swNode.Status),
        OnlineTime: swNode.LastSystemUpTimePollUtc,
        
        // 来源标记
        Source:     "solarwinds",
        SourceID:   swNode.NodeID,
    }
    
    // 处理自定义属性
    if swNode.CustomProperties != nil {
        for k, v := range swNode.CustomProperties {
            asset.Tags = append(asset.Tags, Tag{
                Key:   k,
                Value: v,
            })
        }
    }
    
    return asset
}

// 告警规则转换
func (c *SolarWindsConverter) ConvertAlert(swAlert SolarWindsAlert) *AlertRule {
    rule := &AlertRule{
        Name:        swAlert.Name,
        Description: swAlert.Description,
        Level:       c.mapAlertLevel(swAlert.TriggerSeverity),
        Enabled:     swAlert.Enabled,
        
        // 条件转换
        Condition: AlertCondition{
            Metric:  swAlert.MetricName,
            Operator: c.mapOperator(swAlert.TriggerConditionType),
            Threshold: swAlert.Threshold,
            Duration:  swAlert.Duration,
        },
        
        // 通知配置
        NotifyChannels: c.mapNotification(swAlert.Actions),
    }
    
    return rule
}

// 监控配置转换
func (c *SolarWindsConverter) ConvertMonitor(swMonitor SolarWindsMonitor) *MonitorConfig {
    return &MonitorConfig{
        AssetID:     swMonitor.NodeID,
        MetricName:  swMonitor.StatisticName,
        PollInterval: swMonitor.PollInterval,
        Enabled:     swMonitor.Enabled,
    }
}
```

#### 2.2.8 资产合并策略

```
┌─────────────────────────────────────────────────────────────────────┐
│                     资产合并决策流程                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   导入节点数据                                                        │
│        │                                                           │
│        ▼                                                           │
│   ┌─────────────┐                                                  │
│   │ 多维度匹配   │                                                  │
│   └──────┬──────┘                                                  │
│          │                                                          │
│   ┌──────┼──────┬──────┐                                           │
│   │      │      │      │                                           │
│   ▼      ▼      ▼      ▼                                           │
│  IP匹配  主机名匹配 MAC匹配  序列号匹配                              │
│   │      │      │      │                                           │
│   └──────┴──────┬──────┘                                           │
│                 │                                                   │
│                 ▼                                                   │
│          ┌─────────────┐                                           │
│          │ 决策结果   │                                           │
│          └──────┬──────┘                                           │
│                 │                                                   │
│    ┌────────────┼────────────┐                                     │
│    │            │            │                                     │
│    ▼            ▼            ▼                                     │
│  完全匹配    部分匹配    无匹配                                      │
│    │            │            │                                     │
│    ▼            ▼            ▼                                     │
│  合并配置    待人工确认   新建资产                                    │
│  保留历史    进入待处理   加入资产库                                  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

| 匹配场景 | 处理策略 | 说明 |
|----------|----------|------|
| **IP完全匹配** | 合并配置 | 保留两边配置，新平台优先级高 |
| **IP相同，名称不同** | 人工确认 | 可能设备更换或名称变更 |
| **主机名匹配** | 合并配置 | 关联SNMP信息 |
| **MAC匹配** | 合并配置 | 确认为同一设备 |
| **多网卡IP匹配** | 合并接口 | 补充网络接口信息 |
| **无匹配** | 新建 | 作为新资产导入 |

#### 2.2.9 合并配置详情

```go
// 资产合并策略
type MergeStrategy struct {
    // 字段级合并策略
    FieldMergeRules []FieldMergeRule
    
    // 配置级合并
    MonitorConfigMerge    bool  // 监控配置
    AlertConfigMerge      bool  // 告警配置
    CustomPropsMerge      bool  // 自定义属性
    
    // 历史数据处理
    PreserveHistory       bool  // 保留历史监控数据
    HistoryRetentionDays  int   // 历史数据保留天数
}

// 字段合并规则
type FieldMergeRule struct {
    FieldName   string
    OnConflict  string  // "solarwinds_first" / "new_platform_first" / "merge"
    MergeLogic  string  // 合并逻辑
}

// 合并示例
var DefaultMergeRules = []FieldMergeRule{
    {
        FieldName:  "asset_name",
        OnConflict: "merge",
        MergeLogic: "保留两边名称，用分隔符连接",
    },
    {
        FieldName:  "brand",
        OnConflict: "new_platform_first",
    },
    {
        FieldName:  "model",
        OnConflict: "new_platform_first",
    },
    {
        FieldName:  "primary_ip",
        OnConflict: "merge",
        MergeLogic: "都保留到网络接口表",
    },
    {
        FieldName:  "location",
        OnConflict: "solarwinds_first",  // SolarWinds的位置信息通常更准确
    },
    {
        FieldName:  "tags",
        OnConflict: "merge",
        MergeLogic: "合并标签，去重",
    },
}
```

#### 2.2.10 监控配置迁移

| 配置项 | SolarWinds | 本平台 | 处理方式 |
|--------|------------|--------|----------|
| **采集频率** | PollInterval (秒) | PollInterval | 直接映射 |
| **SNMP配置** | Community String | SNMPv2c/v3 | 自动转换 |
| **阈值告警** | Alert Definitions | Alert Rules | 语义转换 |
| **触发条件** | TriggerCondition | Expression | 规则转换 |
| **告警动作** | Actions | Notify Channels | 重新配置 |
| **依赖关系** | Dependencies | Topology Links | 迁移关联 |
| **自定义属性** | CustomProperties | Tags | 映射导入 |

#### 2.2.11 历史数据迁移

| 数据类型 | 处理方式 | 保留期限 |
|----------|----------|----------|
| **性能指标** | 导入TimescaleDB | 默认1年 |
| **告警历史** | 导入告警表 | 默认1年 |
| **事件日志** | 导入审计日志 | 默认1年 |
| **可用性历史** | 导入可用性表 | 默认1年 |

```sql
-- 历史性能数据导入 (简化示例)
INSERT INTO metrics (time, asset_id, metric_name, metric_value, tags)
SELECT 
    m.Timestamp AS time,
    a.id AS asset_id,
    m.MetricName AS metric_name,
    m.Value AS metric_value,
    jsonb_build_object(
        'source', 'solarwinds',
        'original_id', m.OriginalID
    ) AS tags
FROM solarwinds_performance_data m
JOIN assets a ON a.source_id = m.NodeID
WHERE m.Timestamp >= '2024-01-01'
  AND a.source = 'solarwinds';
```

#### 2.2.12 迁移验证清单

| 验证项 | 检查内容 | 状态 |
|--------|----------|------|
| **资产完整性** | 所有节点都已导入 | ☐ |
| **配置完整性** | 监控配置、告警规则完整 | ☐ |
| **关联关系** | 节点-接口-链路关系正确 | ☐ |
| **历史数据** | 历史监控数据已导入 | ☐ |
| **功能验证** | 采集、告警、通知功能正常 | ☐ |
| **性能对比** | 新旧平台数据一致 | ☐ |

#### 2.2.13 渐进式迁移方案

```
阶段一: 并行运行 (第1-4周)
┌─────────────────────────────────────────────────┐
│                                                 │
│   SolarWinds     新平台                          │
│      │              │                            │
│      │   实时同步   │                            │
│      └──────────────┘                            │
│             │                                     │
│   两套系统同时运行，验证新平台数据准确性          │
│                                                 │
└─────────────────────────────────────────────────┘

阶段二: 切换验证 (第5周)
┌─────────────────────────────────────────────────┐
│                                                 │
│   逐步切换监控目标到新平台:                      │
│   1. 非核心系统先切换                          │
│   2. 核心系统后切换                            │
│   3. 保留SolarWinds作为备份                    │
│                                                 │
└─────────────────────────────────────────────────┘

阶段三: 正式切换 (第6周)
┌─────────────────────────────────────────────────┐
│                                                 │
│   新平台成为主监控平台                          │
│   SolarWinds保留历史数据查询能力                │
│                                                 │
└─────────────────────────────────────────────────┘
```

#### 2.2.14 数据库设计 - 迁移记录

```sql
-- 迁移任务表
CREATE TABLE migration_tasks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_name       VARCHAR(100) NOT NULL,
    source_system   VARCHAR(50) NOT NULL DEFAULT 'solarwinds',
    
    -- 迁移范围
    export_config   JSONB NOT NULL,                    -- 导出配置
    mapping_config  JSONB NOT NULL,                     -- 映射配置
    merge_strategy  JSONB NOT NULL,                    -- 合并策略
    
    -- 进度
    status          VARCHAR(20) DEFAULT 'pending',     -- pending/running/completed/failed
    progress        INTEGER DEFAULT 0,                  -- 百分比
    
    -- 统计
    total_nodes     INTEGER DEFAULT 0,
    imported_nodes  INTEGER DEFAULT 0,
    merged_nodes    INTEGER DEFAULT 0,
    skipped_nodes   INTEGER DEFAULT 0,
    failed_nodes    INTEGER DEFAULT 0,
    
    -- 时间
    started_at      TIMESTAMP,
    completed_at     TIMESTAMP,
    error_message   TEXT,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id)
);

-- 迁移节点状态表
CREATE TABLE migration_node_status (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES migration_tasks(id),
    
    -- 来源信息
    source_id       VARCHAR(100) NOT NULL,            -- SolarWinds NodeID
    source_ip       INET,
    source_name     VARCHAR(255),
    
    -- 匹配状态
    match_type      VARCHAR(20),                       -- exact/partial/none
    matched_asset_id UUID REFERENCES assets(id),
    
    -- 迁移状态
    status          VARCHAR(20) DEFAULT 'pending',     -- pending/migrated/merged/skipped/failed
    
    -- 详情
    asset_id        UUID REFERENCES assets(id),
    error_message   TEXT,
    
    -- 时间
    processed_at    TIMESTAMP,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 迁移历史表
CREATE TABLE migration_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID NOT NULL REFERENCES migration_tasks(id),
    
    -- 操作
    operation       VARCHAR(50) NOT NULL,             -- export/convert/import/merge
    entity_type     VARCHAR(50),                       -- asset/alert/monitor
    
    -- 内容
    source_data     JSONB,
    target_data     JSONB,
    
    -- 结果
    success         BOOLEAN,
    error_message   TEXT,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

### 2.2.15 VMware vCenter集成模块

#### 2.2.15.1 集成概述

对于虚拟化环境，通过vCenter API直接获取ESXi主机和虚拟机的完整信息。

```
┌─────────────────────────────────────────────────────────────────────┐
│                   VMware vCenter集成架构                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────┐                                              │
│   │  vCenter       │                                              │
│   │  Server        │                                              │
│   │                │                                              │
│   │  ┌───────────┐│                                              │
│   │  │ ESXi主机 ││                                              │
│   │  └─────┬─────┘│                                              │
│   │        │       │                                              │
│   │   ┌────┴────┐ │                                              │
│   │   │  虚拟机  │ │                                              │
│   │   └─────────┘ │                                              │
│   └───────┬───────┘                                              │
│           │                                                       │
│           │ vSphere REST API                                      │
│           │                                                       │
│           ▼                                                       │
│   ┌─────────────────┐                                              │
│   │  vCenter适配器  │                                              │
│   │                 │                                              │
│   │  ┌─────────────┐│                                              │
│   │  │ ESXi采集器  ││                                              │
│   │  └─────────────┘│                                              │
│   │  ┌─────────────┐│                                              │
│   │  │ VM采集器    ││                                              │
│   │  └─────────────┘│                                              │
│   │  ┌─────────────┐│                                              │
│   │  │ 集群采集器  ││                                              │
│   │  └─────────────┘│                                              │
│   └────────┬────────┘                                              │
│            │                                                        │
│            ▼                                                        │
│   ┌─────────────────┐                                              │
│   │   资产管理平台  │                                              │
│   └─────────────────┘                                              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

#### 2.2.15.2 连接配置

```yaml
# vmware-integration-config.yaml
# VMware vCenter集成配置

vmware:
  # vCenter连接配置
  connections:
    - name: "生产环境vCenter"
      host: "vcenter-prod.company.com"
      port: 443
      username: "admin@vsphere.local"
      password: "${VCENTER_PASSWORD}"
      
      # 认证方式
      auth_method: "userpass"  # userpass/sso
      
      # SSL配置
      ssl_verify: true
      ca_cert_path: "/etc/ssl/certs/vcenter-ca.crt"
      
      # 连接池
      max_connections: 5
      connection_timeout: 30  # 秒
      request_timeout: 60     # 秒
      
      # 数据获取范围
      scope:
        datacenters: ["北京DC", "上海DC"]
        clusters: ["*"]  # 或指定 ["Cluster-A", "Cluster-B"]
        hosts: ["*"]
        vms: ["*"]
        
  # SSO认证配置
  sso:
    enabled: false
    sso_server: "vsphere.local"
    sso_port: 443
    
  # 采集配置
  collection:
    # ESXi主机采集
    host:
      enabled: true
      interval: 300  # 秒
      
    # 虚拟机采集
    vm:
      enabled: true
      interval: 300  # 秒
      
    # 集群采集
    cluster:
      enabled: true
      interval: 600  # 秒
      
    # 数据存储采集
    datastore:
      enabled: true
      interval: 600  # 秒
      
    # 资源池采集
    resource_pool:
      enabled: true
      interval: 600  # 秒
      
  # 性能指标配置
  performance:
    enabled: true
    level: "realtime"  # realtime/realtime+historical
    historical_days: 30  # 历史数据保留天数
    
  # 同步配置
  sync:
    # 自动发现新增虚拟机
    auto_discover_vms: true
    
    # 同步标签
    sync_tags: true
    
    # 保留原有关联关系
    preserve_associations: true
    
    # 忽略的虚拟机模式
    ignore_patterns:
      - "template-*"
      - "vCLS-*"
```

#### 2.2.15.3 vSphere API调用示例

```python
#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
vCenter API客户端
"""

from pyVim import connect
from pyVmomi import vim
import ssl
import time


class VMwareClient:
    def __init__(self, host, port, username, password, ssl_verify=True):
        self.host = host
        self.port = port
        self.username = username
        self.password = password
        self.ssl_verify = ssl_verify
        self.si = None
        self.content = None
        
    def connect(self):
        """连接vCenter"""
        context = ssl.create_default_context()
        if not self.ssl_verify:
            context.check_hostname = False
            context.verify_mode = ssl.CERT_NONE
            
        self.si = connect.SmartConnect(
            host=self.host,
            user=self.username,
            pwd=self.password,
            port=self.port,
            sslContext=context
        )
        
        self.content = self.si.RetrieveContent()
        print(f"已连接到 vCenter: {self.host}")
        
    def disconnect(self):
        """断开连接"""
        if self.si:
            connect.Disconnect(self.si)
            print(f"已断开 vCenter: {self.host}")
            
    def get_all_hosts(self):
        """获取所有ESXi主机"""
        hosts = []
        container = self.content.viewManager.CreateContainerView(
            self.content.rootFolder,
            [vim.HostSystem],
            True
        )
        
        for host in container.view:
            hosts.append(self._parse_host(host))
            
        return hosts
    
    def _parse_host(self, host):
        """解析ESXi主机信息"""
        hardware = host.config
        network = host.network
        perf_manager = self.content.perfManager
        
        return {
            'name': host.name,
            'full_name': host.summary.config.fullName,
            'vendor': host.summary.hardware.vendor,
            'model': host.summary.hardware.model,
            'uuid': host.summary.hardware.uuid,
            'serial_number': host.summary.hardware.serialNumber,
            
            'cpu_cores': host.summary.hardware.numCpuCores,
            'cpu_threads': host.summary.hardware.numCpuThreads,
            'cpu_mhz': host.summary.hardware.cpuMhz,
            'memory_total': host.summary.hardware.memorySize / 1024 / 1024,
            
            'network_info': {
                'ipv4': [nic.ipAddress for nic in host.config.network.vnic],
                'macs': [nic.mac for nic in host.config.network.vnic],
            },
            
            'esxi_version': host.config.product.version,
            'esxi_build': host.config.product.build,
            
            'runtime': {
                'status': str(host.runtime.connectionState),
                'power_state': str(host.runtime.powerState),
                'uptime': host.runtime.bootTime,
            },
            
            'vms_count': len(host.vm),
        }
    
    def get_all_vms(self):
        """获取所有虚拟机"""
        vms = []
        container = self.content.viewManager.CreateContainerView(
            self.content.rootFolder,
            [vim.VirtualMachine],
            True
        )
        
        for vm in container.view:
            vms.append(self._parse_vm(vm))
            
        return vms
    
    def _parse_vm(self, vm):
        """解析虚拟机信息"""
        hardware = vm.config.hardware
        guest = vm.guest
        
        return {
            'name': vm.name,
            'instance_uuid': vm.config.instanceUuid,
            'bios_uuid': vm.config.uuid,
            
            'host_name': vm.runtime.host.name if vm.runtime.host else None,
            'cluster_name': self._get_cluster_name(vm),
            
            'cpu_cores': hardware.numCPU,
            'memory_mb': hardware.memoryMB,
            
            'disks': [
                {
                    'label': disk.device.deviceInfo.label,
                    'capacity': disk.capacityInKB / 1024 / 1024,
                    'type': str(disk.diskType),
                }
                for disk in hardware.device
                if isinstance(disk, vim.VirtualDisk)
            ],
            
            'nics': [
                {
                    'label': nic.device.deviceInfo.label,
                    'network': nic.networkName,
                    'mac': nic.macAddress,
                    'connected': nic.connectable.connected,
                }
                for nic in hardware.device
                if isinstance(nic, vim.VirtualEthernetCard)
            ],
            
            'guest_full_name': guest.guestFullName,
            'power_state': str(vm.runtime.powerState),
            'ip_addresses': guest.ipAddress,
            
            'cpu_usage': vm.summary.quickStats.cpuUsage,
            'memory_usage': vm.summary.quickStats.guestMemoryUsage,
            
            'annotation': vm.config.annotation,
        }
```

#### 2.2.15.4 数据库设计 - vCenter集成

```sql
-- vCenter连接配置表
CREATE TABLE vmware_connections (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    host            VARCHAR(255) NOT NULL,
    port            INTEGER DEFAULT 443,
    username        VARCHAR(255) NOT NULL,
    password        VARCHAR(255) NOT NULL,
    
    auth_method     VARCHAR(20) DEFAULT 'userpass',
    ssl_verify      BOOLEAN DEFAULT TRUE,
    
    status          VARCHAR(20) DEFAULT 'active',
    last_sync      TIMESTAMP,
    sync_error      TEXT,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- ESXi主机表
CREATE TABLE vmware_hosts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id   UUID NOT NULL REFERENCES vmware_connections(id),
    moref           VARCHAR(50) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    vendor          VARCHAR(100),
    model           VARCHAR(100),
    uuid            VARCHAR(100),
    serial_number   VARCHAR(100),
    esxi_version    VARCHAR(50),
    esxi_build      VARCHAR(50),
    cpu_cores       INTEGER,
    cpu_threads     INTEGER,
    memory_total_mb BIGINT,
    cluster_name    VARCHAR(100),
    status          VARCHAR(20),
    power_state    VARCHAR(20),
    vms_count       INTEGER,
    asset_id        UUID REFERENCES assets(id),
    last_sync       TIMESTAMP DEFAULT NOW(),
    
    UNIQUE (connection_id, moref)
);

-- 虚拟机表
CREATE TABLE vmware_vms (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id   UUID NOT NULL REFERENCES vmware_connections(id),
    moref           VARCHAR(50) NOT NULL,
    instance_uuid   VARCHAR(100) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    host_id         UUID REFERENCES vmware_hosts(id),
    cluster_name    VARCHAR(100),
    guest_full_name VARCHAR(255),
    cpu_cores       INTEGER,
    memory_mb       BIGINT,
    power_state     VARCHAR(20),
    ip_addresses    TEXT[],
    mac_addresses   TEXT[],
    cpu_usage       INTEGER,
    memory_usage    INTEGER,
    annotation      TEXT,
    asset_id        UUID REFERENCES assets(id),
    is_template     BOOLEAN DEFAULT FALSE,
    last_sync       TIMESTAMP DEFAULT NOW(),
    
    UNIQUE (connection_id, instance_uuid)
);

-- 同步历史表
CREATE TABLE vmware_sync_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id   UUID NOT NULL REFERENCES vmware_connections(id),
    sync_type       VARCHAR(20) NOT NULL,
    hosts_added     INTEGER DEFAULT 0,
    hosts_updated   INTEGER DEFAULT 0,
    vms_added       INTEGER DEFAULT 0,
    vms_updated     INTEGER DEFAULT 0,
    errors          INTEGER DEFAULT 0,
    status          VARCHAR(20) DEFAULT 'running',
    started_at      TIMESTAMP DEFAULT NOW(),
    completed_at    TIMESTAMP,
    duration_ms     INTEGER
);
```

#### 2.2.15.5 API设计 - vCenter管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/vmware/connections | 获取vCenter连接列表 |
| POST | /api/v1/vmware/connections | 创建vCenter连接 |
| GET | /api/v1/vmware/connections/:id | 获取连接详情 |
| PUT | /api/v1/vmware/connections/:id | 更新连接配置 |
| DELETE | /api/v1/vmware/connections/:id | 删除连接 |
| POST | /api/v1/vmware/connections/:id/test | 测试连接 |
| POST | /api/v1/vmware/connections/:id/sync | 触发同步 |
| GET | /api/v1/vmware/hosts | 获取ESXi主机列表 |
| GET | /api/v1/vmware/vms | 获取虚拟机列表 |
| GET | /api/v1/vmware/sync-history | 获取同步历史 |


#### 2.2.1 资产分类

| 资产类型 | 示例 |
|----------|------|
| 网络设备 | 路由器、交换机、防火墙、负载均衡器 |
| 服务器 | 物理服务器、虚拟机、容器宿主机 |
| 存储设备 | NAS、SAN存储、存储阵列 |
| 终端设备 | PC、工作站、IP电话、摄像头 |
| 软件资产 | 操作系统、数据库、中间件、应用软件 |

#### 2.2.2 物理部署信息

| 字段 | 说明 |
|------|------|
| 机房名称 | 如"北京数据中心A"、"上海机房B" |
| 机柜编号 | 如"IDC-A-01"、"RACK-15" |
| 机架位置 | U位，如"12U-18U" |
| 承重等级 | 机柜承重能力 |
| 电源配置 | 电源A/B路、PDU端口 |
| 环境信息 | 温度、湿度监控点 |

#### 2.2.3 设备信息

| 字段 | 说明 |
|------|------|
| 设备名称 | 资产标签名 |
| 品牌 | 厂商，如Cisco、华为、H3C、Dell、HP |
| 型号 | 具体型号，如Catalyst 9300、R740 |
| 序列号 | SN号 |
| 购买日期 | 购入时间 |
| 保修期 | 维保截止日期 |
| 供应商 | 采购供应商 |
| 售后服务 | 售后联系方式、服务级别 |
| 维保信息 | 维保状态、续保提醒 |

#### 2.2.4 配置信息（硬件）

| 字段 | 说明 |
|------|------|
| CPU | 型号、核心数、频率 |
| 内存 | 总容量、插槽数、已用槽位 |
| 硬盘 | 数量、容量、类型（SSD/HDD）、RAID配置 |
| 网卡 | 每个网卡：名称、MAC地址、速率、链路状态 |
| 管理口 | IPMI/iLO/iDRAC地址、用户名 |
| 电源 | 电源数量、状态 |
| 风扇 | 风扇数量、转速状态 |

#### 2.2.5 网络信息（多网卡/多IP）

```
设备: Server-01
├── eth0
│   ├── MAC: 00:11:22:33:44:55
│   ├── IPv4: 192.168.1.10/24 (管理网)
│   └── 关联端口: Switch-A-Gi1/0/1
├── eth1
│   ├── MAC: 00:11:22:33:44:56
│   ├── IPv4: 10.0.0.10/8 (业务网)
│   └── 关联端口: Switch-B-Gi2/0/1
├── bond0
│   ├── mode: active-backup
│   ├── slaves: eth2, eth3
│   └── IPv4: 172.16.0.10/16 (存储网)
└──lo (loopback)
    └── IPv4: 127.0.0.1
```

#### 2.2.6 软件信息

| 类别 | 字段 |
|------|------|
| 操作系统 | 名称(RedHat/CentOS/Ubuntu/Windows)、版本、内核版本 |
| 关键组件 | SSH版本、OpenSSL版本、Kernel版本、SSH协议版本 |
| 应用软件 | 软件名称、版本、安装路径、启动用户、端口、状态 |
| 数据库 | MySQL/PostgreSQL/Oracle等，版本、端口、字符集 |
| 中间件 | Nginx/Apache/Tomcat，版本、配置路径 |

#### 2.2.7 资产变更历史

- 记录资产所有字段的变更历史
- 支持时间线回溯
- 变更人、变更原因记录

#### 2.2.8 机房与专线管理

本平台需要对机房、专线等基础设施进行统一管理。

##### 2.2.8.1 机房信息

现有 **3个机房**，通过专线互联：

| 机房 | 用途 | 级别 | 备注 |
|------|------|------|------|
| **中经云机房** | 主生产机房 | 生产 | 核心业务运行 |
| **上地机房** | 灾备机房 | 灾备 | 数据备份、业务容灾 |
| **看丹桥机房** | 测试机房 | 测试 | 开发测试环境 |

**机房网络架构**：

```
┌─────────────────────────────────────────────────────────────────┐
│                    机房网络拓扑                                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────────┐      专线      ┌─────────────────┐       │
│   │   中经云机房    │◄─────────────►│   上地机房      │       │
│   │  (主生产)      │   10Gbps      │   (灾备)        │       │
│   │                 │   专线         │                 │       │
│   └────────┬────────┘               └────────┬────────┘       │
│            │                                  │                │
│            │ 专线                              │ 专线            │
│            │                                  │                │
│   ┌────────▼────────┐               ┌────────▼────────┐       │
│   │  看丹桥机房     │◄─────────────►│   其他区域       │       │
│   │  (测试)        │   专线        │                 │       │
│   └─────────────────┘               └─────────────────┘       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**机房初始化数据**：

```sql
-- 机房初始化数据
INSERT INTO idc (name, code, province, city, address, tier, is_active) VALUES
('中经云机房', 'JZYC', '北京', '北京', '中经云数据中心', '生产', TRUE),
('上地机房', 'SD', '北京', '北京', '上地数据中心', '灾备', TRUE),
('看丹桥机房', 'KDB', '北京', '北京', '看丹桥数据中心', '测试', TRUE);
```

##### 2.2.8.2 专线类型

| 类型 | 说明 | 示例 |
|------|------|------|
| **互联网专线** | 接入互联网的带宽线路 | 100Mbps 互联网专线 |
| **点对点专线** | 两个机房/站点之间的专线 | 中经云 ↔ 上地 10G |
| **MPLS VPN** | 多站点 VPN 专网 | 北京-上海 MPLS VPN |
| **30B+D** | POS机专用线路 | 银行 30B+D 线路 |

##### 2.2.8.3 专线信息管理

专线需要管理的详细信息：

| 字段 | 说明 | 示例 |
|------|------|------|
| **专线编号** | 内部唯一编号 | ZJ-2024-001 |
| **专线类型** | 互联网/点对点/MPLS/30B+D | 点对点专线 |
| **运营商** | 电信/联通/移动/其他 | 中国电信 |
| **带宽** | 带宽大小 | 100Mbps / 10Gbps |
| **端点A** | 起点机房、机柜、设备、接口 | 中经云-A-01-eth0/1 |
| **端点B** | 终点机房、机柜、设备、接口 | 上地-B-02-ge-0/0 |
| **专线用途** | 业务描述 | 生产数据库同步 |
| **业务联系人** | 业务负责人 | 张三 |
| **报障电话** | 运营商报障电话 | 10000 |
| **合同编号** | 运营商合同编号 | CT-2024-001 |
| **到期日期** | 合同到期时间 | 2025-12-31 |
| **状态** | 在用/停用/故障 | 在用 |
| **月租金** | 每月费用(元) | 5000 |

##### 2.2.8.4 专线数据库设计

```sql
-- 专线类型表
CREATE TABLE line_types (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code            VARCHAR(20) NOT NULL UNIQUE,  -- internet/p2p/mpls/30bd
    name            VARCHAR(50) NOT NULL,         -- 互联网专线/点对点专线/MPLS VPN/30B+D
    description     TEXT,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 初始化专线类型
INSERT INTO line_types (code, name) VALUES
('internet', '互联网专线'),
('p2p', '点对点专线'),
('mpls', 'MPLS VPN'),
('30bd', '30B+D');

-- 专线信息表
CREATE TABLE lines (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    line_no         VARCHAR(50) NOT NULL UNIQUE,    -- 专线编号
    line_type_id    UUID REFERENCES line_types(id),
    
    -- 运营商信息
    carrier         VARCHAR(50) NOT NULL,            -- 运营商
    contract_no     VARCHAR(50),                     -- 合同编号
    contract_start  DATE,                           -- 合同开始
    contract_end    DATE,                           -- 合同结束
    monthly_fee     DECIMAL(10,2),                  -- 月租金(元)
    contact_person  VARCHAR(50),                     -- 业务联系人
    contact_phone   VARCHAR(20),                    -- 联系人电话
    fault_phone     VARCHAR(20),                     -- 报障电话
    
    -- 带宽信息
    bandwidth       VARCHAR(30),                     -- 带宽，如 "100Mbps"、"10Gbps"
    bandwidth_mbps  INTEGER,                        -- 带宽数值( Mbps)
    
    -- 端点A
    endpoint_a_idc_id UUID REFERENCES idc(id),
    endpoint_a_rack_id UUID REFERENCES racks(id),
    endpoint_a_device VARCHAR(255),                 -- 设备名称
    endpoint_a_interface VARCHAR(50),               -- 接口名称，如 eth0/1
    endpoint_a_vlan   INTEGER,                     -- VLAN ID
    
    -- 端点B
    endpoint_b_idc_id UUID REFERENCES idc(id),
    endpoint_b_rack_id UUID REFERENCES racks(id),
    endpoint_b_device VARCHAR(255),
    endpoint_b_interface VARCHAR(50),
    endpoint_b_vlan   INTEGER,
    
    -- 业务信息
    purpose         VARCHAR(200),                   -- 用途描述
    business_unit    VARCHAR(50),                   -- 业务部门
    is_critical     BOOLEAN DEFAULT FALSE,         -- 是否关键线路
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active',   -- active/inactive/fault
    install_date    DATE,                          -- 开通日期
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_lines_no ON lines(line_no);
CREATE INDEX idx_lines_type ON lines(line_type_id);
CREATE INDEX idx_lines_carrier ON lines(carrier);
CREATE INDEX idx_lines_status ON lines(status);

-- 专线变更记录
CREATE TABLE line_changes (
    id              UUID PRIMARY KEY,
    line_id         UUID NOT NULL REFERENCES lines(id),
    
    change_type     VARCHAR(20) NOT NULL,           -- bandwidth/status/endpoint
    change_field    VARCHAR(50),
    old_value       TEXT,
    new_value       TEXT,
    
    reason          TEXT,
    operator_id     UUID REFERENCES users(id),
    changed_at      TIMESTAMP DEFAULT NOW()
);

-- 专线监控（可选，监控线路状态）
CREATE TABLE line_monitors (
    id              UUID PRIMARY KEY,
    line_id         UUID NOT NULL REFERENCES lines(id),
    
    monitor_type    VARCHAR(20),                    -- ping/interface/bgp
    target          VARCHAR(100),                  -- 监控目标
    interval        INTEGER DEFAULT 60,            -- 监控间隔(秒)
    
    is_enabled      BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);
```

##### 2.2.8.5 专线可视化

**专线列表**：

```
专线列表：

┌─────────────────────────────────────────────────────────────────┐
│  🔍 搜索专线          类型: [全部 ▼]  状态: [全部 ▼]           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  专线编号   │ 类型      │ 运营商 │ 带宽    │ 端点A→端点B     │ 状态 │
│  ─────────────────────────────────────────────────────────────────│
│  ZJ-001    │ 互联网专线 │ 电信   │ 100Mbps │ 中经云→互联网  │ 正常 │
│  ZJ-002    │ 点对点专线 │ 联通   │ 10Gbps  │ 中经云→上地   │ 正常 │
│  ZJ-003    │ MPLS VPN   │ 移动   │ 50Mbps  │ 中经云→上海    │ 正常 │
│  ZJ-004    │ 30B+D     │ 电信   │ 2Mbps   │ 看丹桥→银行    │ 正常 │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**专线详情**：

```
专线详情：ZJ-002 (中经云 ↔ 上地 10Gbps点对点专线)

┌─────────────────────────────────────────────────────────────────┐
│  基本信息                    │  运营商信息                       │
│  ─────────────────────────── │ ─────────────────────────────    │
│  专线编号: ZJ-002           │  运营商: 中国联通                  │
│  类型: 点对点专线           │  合同编号: LT-2024-001           │
│  带宽: 10Gbps               │  合同期: 2024-01 ~ 2025-12        │
│  状态: 在用                │  月租金: ¥15,000                  │
│  用途: 生产数据同步        │  报障电话: 10010                  │
├─────────────────────────────────────────────────────────────────┤
│  端点信息                                                         │
│  ───────────────────────────────────────────────────────────     │
│  端点A: 中经云机房 - A-01机柜 - Core-SW-01 - eth0/1            │
│  端点B: 上地机房   - B-02机柜 - Core-SW-02 - ge-0/0             │
├─────────────────────────────────────────────────────────────────┤
│  业务信息                                                         │
│  ───────────────────────────────────────────────────────────     │
│  业务部门: 运维部                                                │
│  联系人: 张三 (138****8888)                                      │
│  备注: 主备链路，日常流量约 2Gbps                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 2.3 监控模块

#### 2.3.1 采集方式

| 方式 | 协议 | 适用场景 |
|------|------|----------|
| SNMP主动采集 | SNMP v1/v2c/v3 | 网络设备、打印机、环境监控 |
| Agent推送 | HTTP/API | 服务器、应用（详细指标） |
| SSH远程 | SSH+命令 | 无Agent设备的临时采集 |
| IPMI/Redfish | IPMI协议 | 硬件健康状态 |
| 日志收集 | Syslog/Filebeat | 系统日志、应用日志 |

#### 2.3.2 监控指标

**网络设备**：
- 端口状态、流量、丢包、错误
- CPU/内存使用率
- 温度、电源、风扇状态
- BGP/OSPF邻居状态
- VLAN、STP状态

**服务器**：
- CPU使用率、负载、进程数
- 内存使用率、Swap
- 磁盘使用率、IOPS
- 网卡流量、连接数
- 服务进程状态
- 关键端口可达性

**存储设备**：
- 存储池容量、利用率
- LUN/卷状态
- 控制器状态
- 磁盘健康状态

**应用**：
- 响应时间、QPS
- 错误率
- 连接池状态
- 队列长度

#### 2.3.3 采集频率

| 指标类型 | 默认频率 | 可配置范围 |
|----------|----------|------------|
| 基础指标 | 60秒 | 10秒-30分钟 |
| 网络端口 | 60秒 | 30秒-30分钟 |
| 详细性能 | 300秒 | 1分钟-1小时 |
| 日志 | 实时 | - |

#### 2.3.4 流量分析与故障定位

> **业务痛点**：某业务报失败率增加，但很难定位到具体故障点。

本平台集成**网络流量分析**能力，帮助快速定位故障点。

##### 2.3.4.1 现有设备整合

对于已部署的科莱(Kolle)流量分析系统，可以整合接入：

| 现有资源 | 用途 | 整合方式 |
|----------|------|----------|
| 网络流量采集点 | 交换机镜像流量 | API 接入/NetFlow/sFlow |
| 服务器端口采样 | TCP通讯采样 | Agent/日志接入 |
| 流量探针 | 链路流量监控 | API 接入 |

##### 2.3.4.2 流量分析能力

| 功能 | 说明 |
|------|------|
| **全链路追踪** | 从入口到出口完整路径可视化 |
| **流量异常检测** | 丢包率、延迟、错误码异常告警 |
| **TOP N 分析** | 按流量/请求数/错误数排名 |
| **会话分析** | TCP会话详情、重传分析 |
| **业务关联** | 关联业务系统，快速定位故障服务 |

##### 2.3.4.3 故障定位流程

```
业务报障: "XX业务失败率增加"
     │
     ▼
1. 确认故障时间窗口
     │
     ▼
2. 业务流量分析
   ├── 错误码分布
   ├── 响应时间分布
   ├── 失败请求特征
     │
     ▼
3. 网络路径分析
   ├── 链路流量状态
   ├── 延迟分布
   ├── 丢包/重传检测
     │
     ▼
4. 端点分析
   ├── 服务器资源状态
   ├── 端口连接状态
   ├── 应用日志关联
     │
     ▼
5. 定位结论
   └── 输出故障点、影响范围、建议操作
```

##### 2.3.4.4 流量采集点管理

```sql
-- 流量采集点表
CREATE TABLE traffic_sensors (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    
    -- 采集点位置
    idc_id          UUID REFERENCES idc(id),
    rack_id         UUID REFERENCES racks(id),
    
    -- 采集类型
    sensor_type     VARCHAR(20) NOT NULL,    -- network/server/mixed
    capture_type    VARCHAR(20),             -- mirror/tap/agent/api
    
    -- 网络采集
    target_device   VARCHAR(255),             -- 目标设备
    target_port     VARCHAR(50),             -- 镜像端口/TAP接口
    protocol        VARCHAR(20),             -- netflow/sflow/pcap
    
    -- 服务器采集
    target_server   VARCHAR(255),            -- 目标服务器
    target_ports    INTEGER[],                -- 监控端口列表
    
    -- 接入配置
    collector_addr  VARCHAR(255),             -- 采集器地址
    api_endpoint    VARCHAR(500),             -- API接入地址
    api_key         VARCHAR(255),             -- API密钥（加密）
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active',
    last_data_at    TIMESTAMP,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 流量采样配置
CREATE TABLE traffic_sampling (
    id              UUID PRIMARY KEY,
    sensor_id       UUID REFERENCES traffic_sensors(id),
    
    -- 采样规则
    sample_type     VARCHAR(20),             -- full/percentage/conditional
    sample_rate     INTEGER,                 -- 采样比例 1-100
    filter_rules    JSONB,                   -- 过滤条件
    
    -- 保留策略
    retention_days  INTEGER DEFAULT 7,
    
    is_enabled      BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);
```

##### 2.3.4.5 轻量级流量采集点部署规划

采用 **vnStat + iftop + nethogs** 轻量级方案，在关键位置部署流量探针：

###### 采集工具说明

| 工具 | 功能 | 资源占用 | 数据存储 |
|------|------|----------|----------|
| **vnStat** | 网卡级流量统计 | 极低 | 本地 SQLite |
| **iftop** | TCP/UDP 会话排行 | 低 | 实时/临时 |
| **nethogs** | 按进程流量 | 低 | 实时/临时 |
| **darkstat** | Web 可视化面板 | 中 | 本地 |

> **特点**：只记录流量统计，**不存储抓包数据**，适合大规模部署。

###### 推荐部署位置

| 机房 | 部署位置 | 采集目标 | 工具配置 |
|------|----------|----------|----------|
| **中经云** | 互联网出口 | 电信/联通/移动专线总带宽 | vnStat + darkstat |
| **中经云** | 核心交换机 | 各 VLAN 镜像口流量 | vnStat |
| **中经云** | 关键服务器 | 业务进程流量 | nethogs |
| **中经云** | 专线入口 | 中经云↔上地 10G | vnStat |
| **中经云** | 专线入口 | 中经云↔看丹桥 | vnStat |
| **上地** | 互联网出口 | 灾备出口带宽 | vnStat + darkstat |
| **上地** | 核心交换机 | 各 VLAN 镜像口流量 | vnStat |
| **上地** | 专线入口 | 上地↔中经云 10G | vnStat |
| **看丹桥** | 互联网出口 | 测试环境出口 | vnStat |
| **看丹桥** | 30B+D 专线 | 银行 POS 线路 | vnStat |

###### 部署架构

```
┌─────────────────────────────────────────────────────────────────┐
│                    流量采集点部署架构                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   中经云机房 (生产)                                              │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │ 互联网出口 ──▶ vnStat (eth0/1) ──▶ darkstat Web        │   │
│   │                                                     │   │
│   │ 核心交换机镜像 ──▶ vnStat ──▶ 上报平台               │   │
│   │                                                     │   │
│   │ 关键服务器 ──▶ nethogs ──▶ 进程流量                 │   │
│   │   (订单/支付/数据库)                                  │   │
│   │                                                     │   │
│   │ 专线入口 ──▶ vnStat ──▶ 专线带宽监控                │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│        │ 专线 10Gbps                                           │
│        ▼                                                        │
│   上地机房 (灾备)                                                │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │ 互联网出口 ──▶ vnStat                                    │   │
│   │ 专线入口 ──▶ vnStat ──▶ 专线带宽监控                    │   │
│   │ 核心交换机 ──▶ vnStat                                    │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│        │ 专线                                                  │
│        ▼                                                        │
│   看丹桥机房 (测试)                                              │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │ 互联网出口 ──▶ vnStat                                    │   │
│   │ 30B+D 专线 ──▶ vnStat (银行线路)                        │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│                          │                                       │
│                          ▼ 数据上报                               │
│                 ┌──────────────────┐                           │
│                 │   流量分析平台    │                           │
│                 │  (统一汇聚分析)   │                           │
│                 └──────────────────┘                           │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

###### 数据上报方式

| 方式 | 说明 | 适用场景 |
|------|------|----------|
| **API 推送** | 定时调用平台 API 上报数据 | 推荐 |
| **Syslog** | 发送流量数据到日志服务器 | 现有日志系统 |
| **Prometheus Exporter** | 暴露指标供 Prometheus 拉取 | 已有 Prometheus |

```bash
# vnStat 数据上报示例
#!/bin/bash
# 定时上报流量数据到平台

API_URL="https://monitor.example.com/api/v1/traffic/metrics"
API_KEY="your-api-key"

# 获取网卡流量
for iface in eth0 eth1; do
    vnstat -i $iface --oneline | while read line; do
        # 解析并上报
        curl -X POST $API_URL \
            -H "Authorization: Bearer $API_KEY" \
            -d "{\"interface\":\"$iface\",\"data\":\"$line\"}"
    done
done
```

###### 采集点初始化数据

```sql
-- 流量采集点初始化数据
INSERT INTO traffic_sensors (name, idc_id, sensor_type, capture_type, target_device, protocol, status) VALUES
-- 中经云机房
('中经云-互联网出口-电信', (SELECT id FROM idc WHERE code='JZYC'), 'network', 'mirror', 'Core-SW-01', 'vnstat', 'active'),
('中经云-互联网出口-联通', (SELECT id FROM idc WHERE code='JZYC'), 'network', 'mirror', 'Core-SW-01', 'vnstat', 'active'),
('中经云-核心交换机-VLAN10', (SELECT id FROM idc WHERE code='JZYC'), 'network', 'mirror', 'Core-SW-02', 'vnstat', 'active'),
('中经云-专线-上地', (SELECT id FROM idc WHERE code='JZYC'), 'network', 'interface', 'Border-RTR-01', 'vnstat', 'active'),
('中经云-订单服务器', (SELECT id FROM idc WHERE code='JZYC'), 'server', 'agent', 'app-order-01', 'nethogs', 'active'),
('中经云-支付服务器', (SELECT id FROM idc WHERE code='JZYC'), 'server', 'agent', 'app-pay-01', 'nethogs', 'active'),
('中经云-数据库服务器', (SELECT id FROM idc WHERE code='JZYC'), 'server', 'agent', 'db-mysql-01', 'nethogs', 'active'),
-- 上地机房
('上地-互联网出口', (SELECT id FROM idc WHERE code='SD'), 'network', 'mirror', 'Core-SW-01', 'vnstat', 'active'),
('上地-专线-中经云', (SELECT id FROM idc WHERE code='SD'), 'network', 'interface', 'Border-RTR-01', 'vnstat', 'active'),
-- 看丹桥机房
('看丹桥-互联网出口', (SELECT id FROM idc WHERE code='KDB'), 'network', 'mirror', 'Core-SW-01', 'vnstat', 'active'),
('看丹桥-30BD-银行', (SELECT id FROM idc WHERE code='KDB'), 'network', 'interface', 'POS-RTR-01', 'vnstat', 'active');
```

###### 采集点状态监控

每个采集点需要被监控，确保数据正常上报：

```sql
-- 采集点健康检查
CREATE TABLE traffic_sensor_health (
    id              UUID PRIMARY KEY,
    sensor_id       UUID REFERENCES traffic_sensors(id),
    
    check_time      TIMESTAMPTZ NOT NULL,
    
    -- 检查结果
    is_online      BOOLEAN DEFAULT TRUE,
    last_report_at TIMESTAMP,                -- 上次上报时间
    data_freshness INTEGER,                  -- 数据新鲜度(秒)
    
    -- 流量统计
    rx_bytes       BIGINT,
    tx_bytes       BIGINT,
    
    -- 异常信息
    error_message   TEXT,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 告警规则：采集点离线超过5分钟
INSERT INTO alert_rules (name, metric_name, condition, threshold, severity, duration)
VALUES ('流量采集点离线', 'sensor_online', 'eq', 0, 'warning', 300);
```

###### 运维管理

| 任务 | 频率 | 说明 |
|------|------|------|
| **采集点巡检** | 每日 | 检查在线状态、数据上报 |
| **流量基线更新** | 每周 | 更新各时段流量基线 |
| **阈值调优** | 每月 | 根据历史数据调整告警阈值 |
| **采集点扩缩** | 按需 | 新增业务/机房时部署 |

---

-- 业务流量基线
CREATE TABLE traffic_baselines (
    id              UUID PRIMARY KEY,
    service_name    VARCHAR(100) NOT NULL,
    
    -- 基线指标
    avg_rps         DECIMAL(10,2),          -- 平均QPS
    avg_latency     DECIMAL(10,2),          -- 平均延迟(ms)
    error_rate_max  DECIMAL(5,4),           -- 最大错误率
    p99_latency     DECIMAL(10,2),          -- P99延迟
    
    -- 时间范围
    time_range      VARCHAR(20),             -- peak/offpeak
    
    -- 更新
    updated_at      TIMESTAMP DEFAULT NOW()
);
```

##### 2.3.4.5 故障分析界面

```
故障分析面板：

┌─────────────────────────────────────────────────────────────────┐
│  业务故障分析: 订单服务                              时间: 14:00│
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  业务概览:                                                       │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ 总请求: 1,234,567  失败: 12,345 (1.0%)  ↑  ↑               ││
│  │ 响应时间: P50=120ms  P99=850ms  ↑  ↑                       ││
│  └─────────────────────────────────────────────────────────────┘│
│                                                                  │
│  错误码分布:         TOP 5 慢请求:                               │
│  ┌───────────┐       ┌───────────────┐                          │
│  │ 504: 45%  │       │ /api/order    │  2.3s                  │
│  │ 500: 30%  │       │ /api/pay      │  1.8s                  │
│  │ 503: 20%  │       │ /api/query    │  1.5s                  │
│  │ other: 5% │       └───────────────┘                          │
│  └───────────┘                                                  │
│                                                                  │
│  网络路径分析:                                                    │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │  ─── 负载均衡 ───  ── 应用服务器 ──  ── 数据库 ──        ││
│  │    LB-01           APP-03            DB-01                 ││
│  │  ↓ 100%           ↓ 95%            ↓ 12% (高)             ││
│  │  延迟: 5ms         延迟: 800ms        延迟: 120ms            ││
│  │                     ⚠️ 异常                              ││
│  └─────────────────────────────────────────────────────────────┘│
│                                                                  │
│  定位结论:                                                        │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ 🔴 故障点: APP-03 服务器                                    ││
│  │    原因: 数据库连接池耗尽                                    ││
│  │    建议: 重启服务/扩容                                       ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

##### 2.3.4.6 与现有系统对接

对于科莱等现有流量分析系统：

```sql
-- 外部流量系统对接配置
CREATE TABLE traffic_integrations (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    vendor          VARCHAR(50) NOT NULL,       -- kolle/其他
    
    -- 对接配置
    api_url         VARCHAR(500),
    api_key         VARCHAR(255),
    
    -- 数据映射
    metric_mapping  JSONB,                       -- 字段映射关系
    
    -- 同步配置
    sync_interval   INTEGER DEFAULT 300,         -- 同步间隔(秒)
    last_sync_at   TIMESTAMP,
    
    status          VARCHAR(20) DEFAULT 'active',
    created_at      TIMESTAMP DEFAULT NOW()
);
```

### 2.4 告警模块

#### 2.4.1 告警规则

```
告警规则示例:
├── CPU使用率 > 80% 持续 5分钟  -> 告警级别: 警告
├── CPU使用率 > 95% 持续 1分钟  -> 告警级别: 严重
├── 磁盘使用率 > 90%           -> 告警级别: 警告
├── 端口Down                   -> 告警级别: 严重
├── 内存使用率 > 85%           -> 告警级别: 警告
└── 响应时间 > 5000ms          -> 告警级别: 警告
```

#### 2.4.2 告警级别

| 级别 | 颜色 | 说明 |
|------|------|------|
| 信息(Info) | 蓝色 | 一般信息，无需处理 |
| 警告(Warning) | 黄色 | 需要关注 |
| 严重(Critical) | 红色 | 需要立即处理 |
| 紧急(Emergency) | 紫红 | 最高优先级 |

#### 2.4.3 告警方式

| 方式 | 配置参数 | 说明 |
|------|----------|------|
| 钉钉 | Webhook URL、关键字 | 支持@人、卡片格式 |
| 企业微信 | Webhook URL、AgentID | 支持卡片消息 |
| 邮件 | SMTP配置、收件人 | 支持HTML格式、附件 |
| 短信 | SMS网关API | 紧急告警使用 |
| 语音 | 电话网关/TTS | 最高级别告警触发 |
| Webhook | URL、HTTP方法、Header | 集成第三方系统 |

#### 2.4.4 告警策略

- **告警抑制**：同一告警短时间内不重复通知
- **告警升级**：告警未处理时自动升级
- **告警合并**：多个相关告警合并为一条
- **告警静默**：维护窗口期间静默
- **告警关联**：关联相关设备，减少噪音

### 2.5 可视化模块

#### 2.5.1 机柜可视化

```
┌──────────────────────────────────────┐
│  机柜: IDC-A-01          机房: 北京A  │
├──────────────────────────────────────┤
│ ┌──────────────────────────────┐     │
│ │ 1U - Switch-A (Cisco 9300)  │     │
│ │ [●] Gi1/0/1  [●] Gi1/0/2   │     │
│ └──────────────────────────────┘     │
│ ┌──────────────────────────────┐     │
│ │ 2U - Firewall-A (Fortinet)  │     │
│ │ [●] Port1  [●] Port2       │     │
│ └──────────────────────────────┘     │
│           ...                        │
│ ┌──────────────────────────────┐     │
│ │ 12U - Server-01 (Dell R740)│     │
│ │ CPU: ████████░░ 80%        │     │
│ │ MEM: ██████████ 95%  [⚠️]   │     │
│ │ DISK: ███████░░░ 70%       │     │
│ └──────────────────────────────┘     │
│ ┌──────────────────────────────┐     │
│ │ 13U - Server-02 (Dell R740)│     │
│ │ [正常]                     │     │
│ └──────────────────────────────┘     │
└──────────────────────────────────────┘
```

特性：
- 3D/2.5D机柜渲染
- 实时显示设备状态
- 拖拽式设备上下架
- 空间利用率统计

##### 2.5.1.1 设备变更操作

机柜图支持以下设备变更操作：

| 操作 | 说明 | 触发方式 |
|------|------|----------|
| **上架** | 新设备放入空槽位 | 拖拽 / 点击"上架" |
| **下架** | 设备从机柜移除 | 右键菜单 / 拖出 |
| **移动** | 设备在机柜内/间移动 | 拖拽到新位置 |
| **归还** | 下架后设备归还资产库 | 确认归还 |

##### 2.5.1.2 设备上架

```
上架流程：

1. 选择机柜 → 查看空槽位
          │
          ▼
2. 选择设备（从资产库 / 导入）
          │
          ▼
3. 拖拽到目标槽位 / 输入U位
          │
          ▼
4. 确认上架信息
   - 设备信息
   - 安装位置
   - 网络连接
          │
          ▼
5. 执行上架 → 更新资产位置
          │
          ▼
6. 自动关联监控
```

##### 2.5.1.3 设备下架

```
下架流程：

1. 选择设备 → 右键"下架"
          │
          ▼
2. 确认下架
   - 是否需要保留资产记录
   - 是否需要备份配置
          │
          ▼
3. 断开连接（如有监控线缆）
          │
          ▼
4. 执行下架 → 设备移至"待归还"
          │
          ▼
5. 记录下架原因
```

##### 2.5.1.4 设备移动

```
移动场景：

┌─────────────────────────────────────────────────────────────┐
│  机柜内移动              机柜间移动                          │
│  ┌─────────┐            ┌─────────┐    ┌─────────┐        │
│  │ 12U → 20U│            │ IDC-A  │ →  │ IDC-B   │        │
│  └─────────┘            └─────────┘    └─────────┘        │
└─────────────────────────────────────────────────────────────┘

移动流程：

1. 选择设备 → 拖拽到新位置
          │
          ▼
2. 确认移动
   - 源位置 → 目标位置
   - 预计停机时间
          │
          ▼
3. 执行移动
   - 断开旧连接
   - 安装到新位置
   - 连接新网络/电源
          │
          ▼
4. 更新位置信息 → 同步到资产库
```

##### 2.5.1.5 交互界面

```
机柜图交互：

┌─────────────────────────────────────────────────────────────────┐
│  机柜: IDC-A-01  [📊 统计] [⚙️ 设置] [🔙 返回]                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ [拖拽此处上架]                              [拖出下架]    │  │
│  ├───────────────────────────────────────────────────────────┤  │
│  │ 42U ┌─────────────────────────────────────────────────┐ │  │
│  │     │  1U  │  ●  │ Switch-01 (Cisco 9300)  [⋮]       │ │  │
│  │     ├──────┼──────┼──────────────────────────────────┤ │  │
│  │     │  2U  │  ●  │ Firewall-A (Fortinet)    [⋮]       │ │  │
│  │     ├──────┼──────┼──────────────────────────────────┤ │  │
│  │     │  3U  │      │                                  │ │  │
│  │     ├──────┼──────┼──────────────────────────────────┤ │  │
│  │     │ ...  │      │                                  │ │  │
│  │     ├──────┼──────┼──────────────────────────────────┤ │  │
│  │     │ 12U  │  ⚠️  │ Server-01 (Dell R740)    [⋮]       │ │  │
│  │     │      │ CPU ↑│  [右键菜单: 下架|移动|详情|SSH]    │ │  │
│  │     ├──────┼──────┼──────────────────────────────────┤ │  │
│  │     │ 13U  │      │  [空闲槽位]                        │ │  │
│  │     │      │      │  [拖拽设备放置此处]                 │ │  │
│  │     └──────┴──────┴──────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  统计: 已用 15U / 42U (35%)  │  设备: 8台  │  告警: 1        │
└─────────────────────────────────────────────────────────────────┘

右键菜单选项：
├── 📊 详情     - 查看设备完整信息
├── 🔄 移动     - 移动到其他位置
├── 📤 下架     - 设备下架
├── 🔌 网络     - 查看网络连接
├── 💻 SSH      - SSH登录（通过堡垒机）
└── 📝 变更记录 - 查看位置变更历史

##### 2.5.1.6 工单中设备位置标识

在工单处理时，执行人、审计员等相关人员需要快速定位目标设备，机柜图和设备信息中必须清晰显示位置信息：

```
┌─────────────────────────────────────────────────────────────────┐
│  工单详情                                                       │
├─────────────────────────────────────────────────────────────────┤
│  标题: Server-01 操作系统安全基线配置                            │
│  ─────────────────────────────────────────────────────────────  │
│  目标设备:                                                     │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │  设备名称: Server-01                                         ││
│  │  IP地址: 192.168.1.10                                       ││
│  │  ─────────────────────────────────────────────────────────  ││
│  │  📍 位置信息:                                                ││
│  │     机柜: IDC-A-01 (北京A机房)                             ││
│  │     机柜位置: 12U - 18U (共6U)                             ││
│  │     [🗺️ 查看机柜图]                                        ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

**位置信息展示**：

| 字段 | 说明 | 示例 |
|------|------|------|
| **机柜** | 机柜编号 + 机房 | IDC-A-01 (北京A机房) |
| **机柜位置** | U位范围 + 占用高度 | 12U - 18U (共6U) |
| **快捷链接** | 点击跳转机柜图 | [🗺️ 查看机柜图] |

**机柜图中设备标识**：

```
┌─────────────────────────────────────────────────────────────────┐
│  机柜: IDC-A-01          🔍 搜索设备                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  42U ┌─────────────────────────────────────────────────────┐   │
│      │  1U  │ ● │ Switch-01 (Cisco 9300)                 │   │
│      ├──────┼──────┼──────────────────────────────────────┤   │
│      │  2U  │ ● │ Firewall-A (Fortinet)                 │   │
│      ├──────┼──────┼──────────────────────────────────────┤   │
│      │ ...  │    │                                       │   │
│      ├──────┼──────┼──────────────────────────────────────┤   │
│  ▶─▶ │ 12U  │ ⚠️ │ Server-01 (Dell R740) ◀── 当前设备  │   │
│      │      │    │ IP: 192.168.1.10                       │   │
│      │      │    │ [工单: 安全基线配置]                    │   │
│      ├──────┼──────┼──────────────────────────────────────┤   │
│      │ 13U  │    │ Server-02 (Dell R740)                 │   │
│      └──────┴──────┴──────────────────────────────────────┘   │
│                                                                  │
│  图例: ● 正常  ⚠️ 告警  ◀─▶ 当前工单目标设备                  │
└─────────────────────────────────────────────────────────────────┘
```

**工单关联设备时的位置提示**：

```
设备选择器：

┌─────────────────────────────────────────────────────────────────┐
│  🔍 搜索设备 (IP/名称/位置)                                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  筛选条件:                                                      │
│  ├── 机房: [全部 ▼]                                             │
│  ├── 机柜: [全部 ▼]                                             │
│  └── 状态: [全部 ▼]                                             │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ ☐ │ Server-01      │ 192.168.1.10 │ IDC-A-01 │ 12U-18U │  │
│  │ ☐ │ Server-02      │ 192.168.1.11 │ IDC-A-01 │ 20U-26U │  │
│  │ ☐ │ Switch-01      │ 192.168.1.1  │ IDC-A-01 │ 1U      │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  列: 设备名称 | IP | 机柜 | U位                                 │
└─────────────────────────────────────────────────────────────────┘
```

**权限说明**：

- **执行人**：查看工单关联设备的机柜位置
- **审计员**：查看所有设备位置（审计用）
- **资产管理员**：可编辑设备位置信息

##### 2.5.1.7 数据库设计

```sql
-- 机柜变更记录表
CREATE TABLE rack_changes (
    id              UUID PRIMARY KEY,
    asset_id        UUID NOT NULL REFERENCES assets(id),
    
    -- 变更类型
    change_type     VARCHAR(20) NOT NULL,   -- install/uninstall/move
    
    -- 变更前
    rack_id_before  UUID REFERENCES racks(id),
    position_before VARCHAR(50),             -- 如 "12U-18U"
    
    -- 变更后
    rack_id_after   UUID REFERENCES racks(id),
    position_after  VARCHAR(50),             -- 如 "20U-26U"
    
    -- 详情
    reason          TEXT,                   -- 下架/移动原因
    operator_id     UUID REFERENCES users(id),
    operated_at     TIMESTAMP DEFAULT NOW(),
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'completed',  -- pending/in_progress/completed/cancelled
    
    -- 工单关联
    ticket_id       UUID REFERENCES tickets(id)
);

CREATE INDEX idx_rack_changes_asset ON rack_changes(asset_id);
CREATE INDEX idx_rack_changes_rack ON rack_changes(rack_id_after);
CREATE INDEX idx_rack_changes_date ON rack_changes(operated_at DESC);

-- 待上架设备池
CREATE TABLE device_staging (
    id              UUID PRIMARY KEY,
    asset_id        UUID REFERENCES assets(id),
    
    -- 目标机柜
    target_rack_id  UUID REFERENCES racks(id),
    target_position VARCHAR(50),             -- 预分配U位
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'pending',  -- pending/installing/installed/cancelled
    
    -- 计划时间
    scheduled_time  TIMESTAMPTZ,
    
    created_at      TIMESTAMP DEFAULT NOW()
);
```

#### 2.5.2 拓扑图

```
                    ┌─────────────┐
                    │   互联网    │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  边界防火墙  │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
        ┌─────▼─────┐ ┌────▼────┐ ┌─────▼─────┐
        │ 核心交换机 │ │ 核心交换 │ │ 核心交换  │
        └─────┬─────┘ └────┬────┘ └─────┬─────┘
              │            │            │
        ┌─────▼─────┐ ┌────▼────┐ ┌─────▼─────┐
        │ 业务VLAN  │ │ 存储VLAN │ │ 管理VLAN  │
        └─────┬─────┘ └────┬────┘ └─────┬─────┘
              │            │            │
        ┌─────▼─────┐ ┌────▼────┐ ┌─────▼─────┐
        │ Server-01 │ │Server-02│ │ Server-03 │
        │  [●]正常  │ │ [●]正常 │ │ [⚠️]告警  │
        └───────────┘ └─────────┘ └───────────┘
```

特性：
- 自动生成拓扑（基于ARP/LLDP/CDP）
- 手动绘制/编辑
- 设备状态实时展示
- 链路流量可视化
- 支持分层（核心层、汇聚层、接入层）

##### 2.5.2.1 设备快捷访问

在拓扑图、资产列表（服务器、虚拟机、网络设备）中，支持快捷点击访问目标设备：

| 访问方式 | 说明 | 适用设备 |
|----------|------|----------|
| **Web 界面** | 点击后打开设备管理界面（iLO/iDRAC/Web控制台） | 服务器、网络设备 |
| **SSH 终端** | 点击后通过**堡垒机**建立 SSH 连接 | 服务器、网络设备 |

```
┌─────────────────────────────────────────────────────────────────┐
│                     设备快捷访问架构                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   用户点击设备 ──▶ 选择访问方式                                   │
│         │                                                        │
│         ├──▶ Web 访问 ──▶ 直接打开设备管理界面                     │
│         │              (iLO / iDRAC / Web控制台)                  │
│         │                                                        │
│         └──▶ SSH 访问 ──▶ 跳转至堡垒机 ──▶ 建立SSH隧道            │
│                            │                                      │
│                            ▼                                      │
│                   ┌──────────────────┐                          │
│                   │     堡垒机        │                          │
│                   │  (Jump Server)   │                          │
│                   │                  │                          │
│                   │  - 身份认证       │                          │
│                   │  - 权限检查       │                          │
│                   │  - 操作审计       │                          │
│                   │  - 会话录像       │                          │
│                   └────────┬─────────┘                          │
│                            │                                      │
│                            ▼                                      │
│                     目标设备 SSH                                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**SSH 访问流程**：

1. 用户点击 "SSH" 按钮
2. 系统检查用户是否有该设备的 SSH 权限
3. 验证通过后，跳转堡垒机并自动打开SSH客户端
4. **提示用户**：在堡垒机搜索框中输入设备 IP 或主机名快速定位

```
┌─────────────────────────────────────────────────────────────────┐
│  跳转后提示用户：                                                │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │  ⚡ 正在连接 Server-01 (192.168.1.10)                        │ │
│  │                                                             │ │
│  │  📋 在堡垒机中搜索以下信息快速定位设备：                       │ │
│  │     → IP: 192.168.1.10                                     │ │
│  │     → 名称: server-01                                       │ │
│  │                                                             │ │
│  │  [连接成功] 5秒后自动跳转...                                │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

**数据库设计**：

```sql
-- 堡垒机配置
CREATE TABLE bastion_hosts (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    host            VARCHAR(255) NOT NULL,         -- 堡垒机地址
    port            INTEGER DEFAULT 22,
    
    -- 连接参数
    jump_host       INET,                         -- 跳转目标（如有）
    
    -- 认证
    auth_method     VARCHAR(20),                  -- password/key/oauth
    credential_id   UUID REFERENCES credentials(id),
    
    is_active       BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 设备访问权限
CREATE TABLE device_access (
    id              UUID PRIMARY KEY,
    user_id         UUID REFERENCES users(id),
    asset_id        UUID REFERENCES assets(id),
    
    -- 访问权限
    can_web         BOOLEAN DEFAULT TRUE,         -- Web访问
    can_ssh         BOOLEAN DEFAULT TRUE,         -- SSH访问
    can_console     BOOLEAN DEFAULT FALSE,        -- 物理console
    
    -- 限制
    ip_whitelist    INET[],                      -- 允许的源IP
    
    -- 审批
    require_approval BOOLEAN DEFAULT FALSE,
    approver_id     UUID REFERENCES users(id),
    
    created_at      TIMESTAMP DEFAULT NOW(),
    
    UNIQUE(user_id, asset_id)
);

-- Web访问配置（iLO/iDRAC等）
CREATE TABLE web_access_configs (
    id              UUID PRIMARY KEY,
    asset_id        UUID REFERENCES assets(id),
    
    -- Web接口配置
    web_protocol    VARCHAR(10) DEFAULT 'https',
    web_port        INTEGER DEFAULT 443,
    web_path        VARCHAR(100),                -- 如 /ilo5/mgt
    
    -- 认证信息（加密存储）
    web_username    VARCHAR(100),
    web_password    VARCHAR(255),                 -- 加密存储
    
    -- 验证
    last_checked    TIMESTAMP,
    is_reachable    BOOLEAN,
    
    created_at      TIMESTAMP DEFAULT NOW()
);
```

**界面交互**：

```
拓扑图节点 / 资产列表行：
┌─────────────────────────────┐
│  🎯 Server-01 (192.168.1.10) │
│  状态: ● 正常                │
│  ─────────────────────────  │
│  [🌐 Web]  [💻 SSH]  [📊 详情] │
└─────────────────────────────┘
```

- **Web 按钮**：直接跳转设备管理界面（iLO/iDRAC/vSphere Client 等）
- **SSH 按钮**：跳转堡垒机发起 SSH 连接

#### 2.5.3 仪表盘

- 自定义仪表盘布局
- 拖拽式组件配置
- 多维度数据展示
- 实时数据刷新

#### 2.5.4 历史趋势图

- 时间范围选择（小时/天/周/月/年）
- 多指标叠加
- 导出图片/PDF
- 对比历史同期

---

## 三、系统架构设计

### 3.1 总体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                         前端展示层 (Web UI)                         │
│   React + TypeScript + Ant Design / Element Plus + ECharts        │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                          API网关层                                   │
│   Nginx/Go + RESTful API + GraphQL + WebSocket                    │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         服务层 (微服务)                              │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐      │
│  │  资产服务   │ │  监控服务   │ │  告警服务   │ │  拓扑服务   │      │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘      │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐      │
│  │ 发现服务   │ │ 采集服务   │ │ 报告服务   │ │  用户服务   │      │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘      │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         数据存储层                                   │
│   ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────┐   │
│   │ PostgreSQL │  │ TimescaleDB│  │  Redis     │  │  MongoDB   │   │
│   │ (资产/配置) │  │ (时序数据) │  │ (缓存/队列) │  │ (日志/拓扑) │   │
│   └────────────┘  └────────────┘  └────────────┘  └────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         采集层                                      │
│   ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐      │
│   │  SNMP采集  │ │  Agent    │ │  SSH采集   │ │  IPMI采集  │      │
│   │  (Go)     │ │  (Go)     │ │  (Go)     │ │  (Go)     │      │
│   └────────────┘ └────────────┘ └────────────┘ └────────────┘      │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         采集目标                                     │
│   网络设备 │ 服务器 │ 存储设备 │ 虚拟机 │ 容器 │ 应用服务          │
└─────────────────────────────────────────────────────────────────────┘
```

### 3.2 部署架构

#### 3.2.1 VMware HA 集群部署

本平台部署于 VMware vSphere 虚拟化环境，利用 vSphere HA 实现基础高可用，无需自行构建主备模式。

```
┌─────────────────────────────────────────────────────────────────────┐
│                      VMware vSphere 集群                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────────┐    ┌─────────────────────┐             │
│   │   ESXi Host 1       │    │   ESXi Host 2       │             │
│   │   ┌─────────────┐   │    │   ┌─────────────┐   │             │
│   │   │  管控节点1   │   │    │   │  管控节点2   │   │             │
│   │   │ (Control-1) │   │    │   │ (Control-2) │   │             │
│   │   └─────────────┘   │    │   └─────────────┘   │             │
│   │   ┌─────────────┐   │    │   ┌─────────────┐   │             │
│   │   │  采集节点1  │   │    │   │  采集节点2  │   │             │
│   │   │ (Agent-1)   │   │    │   │ (Agent-2)   │   │             │
│   │   └─────────────┘   │    │   └─────────────┘   │             │
│   └─────────────────────┘    └─────────────────────┘             │
│              │                          │                         │
│              └──────────┬───────────────┘                         │
│                         │                                          │
│                    ┌────▼────┐                                     │
│                    │ vSphere │                                     │
│                    │   HA   │                                     │
│                    └─────────┘                                     │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                      存储与网络                                      │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────┐    ┌─────────────────┐                     │
│   │  vSAN / NAS     │    │   vDS 分布式    │                     │
│   │  (数据存储)     │    │   (网络虚拟化)  │                     │
│   └─────────────────┘    └─────────────────┘                     │
│                                                                      │
│   ┌─────────────────┐    ┌─────────────────┐                     │
│   │  vCenter        │    │   DRS/DRS       │                     │
│   │  (管理)         │    │   (负载均衡)    │                     │
│   └─────────────────┘    └─────────────────┘                     │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

| 组件 | 部署方式 | 高可用策略 |
|------|----------|------------|
| **管控服务** | 2+ 节点部署，VIP + Keepalived | vSphere HA 自动故障切换 |
| **采集服务** | 多节点部署，无状态 | 采集任务自动分配 |
| **数据库** | PostgreSQL 主从 + TimescaleDB | 应用层实现主从切换 |
| **Redis** | Cluster 模式或主从 | 哨兵监控自动切换 |
| **MongoDB** | Replica Set | 自动故障选主 |

> **说明**：利用 VMware HA + DRS，物理主机故障时虚拟机自动迁移到其他主机，业务不中断。无需再构建复杂的主备复制。

#### 3.2.2 扩缩容设计

| 场景 | 扩容方式 |
|------|----------|
| **采集性能不足** | 增加采集节点，配置采集任务分流 |
| **存储空间不足** | TimescaleDB 冷热数据分离，历史数据归档 |
| **访问量增长** | 增加管控节点，Nginx 负载均衡 |

### 3.3 技术选型

#### 3.3.1 后端技术栈

| 组件 | 技术选型 | 理由 |
|------|----------|------|
| **语言** | Golang | 高性能、并发好、生态丰富 |
| **框架** | Gin/Fiber | 高性能HTTP框架 |
| **ORM** | GORM | Go生态成熟、功能完善 |
| **任务调度** | Chromatic/Cron | 分布式任务调度 |
| **消息队列** | Redis Queue / NATS | 轻量级、高性能 |

#### 3.2.2 数据库选型

| 数据库 | 用途 | 选型理由 |
|--------|------|----------|
| **PostgreSQL** | 资产、配置、用户、告警等结构化数据 | 功能强大、支持JSON、扩展性好 |
| **TimescaleDB** | 时序监控数据 | 基于PostgreSQL、超高性能时序处理 |
| **Redis** | 缓存、实时数据、会话、消息队列 | 高速缓存、发布订阅 |
| **MongoDB** | 日志、拓扑图数据、灵活文档 | 文档型、适合拓扑等图结构数据 |

> **为什么不用 Prometheus + Grafana？**
> 
> Prometheus + Grafana 是很好的监控组合，但：
> 1. 资产管理能力弱，难以满足物理信息、多网卡IP等需求
> 2. 自动发现和资产整合需要额外开发
> 3. 告警需要对接AlertManager，定制化成本高
> 4. 机柜可视化能力有限
> 
> 本方案自建平台，资产+监控+告警+可视化一体化，更贴合企业需求。

#### 3.2.3 前端技术栈

| 组件 | 技术选型 |
|------|----------|
| **框架** | React 18 + TypeScript |
| **UI库** | Ant Design 5 / Element Plus |
| **图表** | ECharts / G2 |
| **拓扑图** | G6 / JointJS |
| **机柜图** | Three.js / D3.js |
| **状态管理** | Zustand / Redux Toolkit |
| **HTTP客户端** | Axios / TanStack Query |
| **构建工具** | Vite |
| **国际化** | i18next + react-i18next |

#### 3.2.4 多语言支持 (i18n)

系统支持多语种界面，默认 **简体中文 (zh-CN)**，可选 **English (en-US)**。

##### 支持的语言

| 语言代码 | 语言名称 | 状态 |
|----------|----------|------|
| zh-CN | 简体中文 | 默认 |
| en-US | English | 可选 |

##### 国际化架构

```
┌─────────────────────────────────────────────────────────────────┐
│                    国际化架构                                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   用户浏览器                                                      │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  语言检测优先级:                                           │   │
│   │  1. URL 参数 (?lang=en)                                 │   │
│   │  2. LocalStorage 保存                                     │   │
│   │  3. 浏览器语言                                            │   │
│   │  4. 默认: zh-CN                                          │   │
│   └─────────────────────────────────────────────────────────┘   │
│                              │                                   │
│                              ▼                                   │
│   React 前端                                                      │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  i18next                                                 │   │
│   │  ├── resources/                                          │   │
│   │  │   ├── zh-CN.json   (简体中文翻译)                    │   │
│   │  │   └── en-US.json   (英文翻译)                        │   │
│   │  │                                                      │   │
│   │  └── i18n.ts (配置)                                     │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

##### 翻译文件结构

```json
// locales/zh-CN.json
{
  "common": {
    "save": "保存",
    "cancel": "取消",
    "confirm": "确认",
    "delete": "删除",
    "edit": "编辑",
    "search": "搜索",
    "export": "导出",
    "import": "导入"
  },
  "menu": {
    "dashboard": "仪表盘",
    "assets": "资产管理",
    "monitor": "监控中心",
    "alerts": "告警管理",
    "tickets": "工单管理",
    "reports": "报表中心",
    "settings": "系统设置"
  },
  "assets": {
    "title": "资产管理",
    "create": "创建资产",
    "edit": "编辑资产",
    "delete": "删除资产",
    "name": "资产名称",
    "ip": "IP地址",
    "type": "资产类型",
    "status": "状态"
  },
  "alerts": {
    "title": "告警管理",
    "critical": "严重",
    "warning": "警告",
    "info": "信息",
    "acknowledge": "确认",
    "resolve": "解决"
  },
  "tickets": {
    "title": "工单管理",
    "create": "创建工单",
    "assign": "指派",
    "close": "关闭",
    "priority": "优先级"
  }
}
```

```json
// locales/en-US.json
{
  "common": {
    "save": "Save",
    "cancel": "Cancel",
    "confirm": "Confirm",
    "delete": "Delete",
    "edit": "Edit",
    "search": "Search",
    "export": "Export",
    "import": "Import"
  },
  "menu": {
    "dashboard": "Dashboard",
    "assets": "Assets",
    "monitor": "Monitor",
    "alerts": "Alerts",
    "tickets": "Tickets",
    "reports": "Reports",
    "settings": "Settings"
  },
  "assets": {
    "title": "Asset Management",
    "create": "Create Asset",
    "edit": "Edit Asset",
    "delete": "Delete Asset",
    "name": "Asset Name",
    "ip": "IP Address",
    "type": "Asset Type",
    "status": "Status"
  },
  "alerts": {
    "title": "Alert Management",
    "critical": "Critical",
    "warning": "Warning",
    "info": "Info",
    "acknowledge": "Acknowledge",
    "resolve": "Resolve"
  },
  "tickets": {
    "title": "Ticket Management",
    "create": "Create Ticket",
    "assign": "Assign",
    "close": "Close",
    "priority": "Priority"
  }
}
```

##### 切换语言

```
┌─────────────────────────────────────────────────────────────────┐
│  界面右上角:                                                    │
│                                                                  │
│  [🔔 告警] [👤 用户] [🌐 EN ▼]                               │
│                                                                  │
│  点击语言选项:                                                   │
│  ┌─────────────────┐                                            │
│  │ 简体中文 (默认) │                                            │
│  │ English         │                                            │
│  └─────────────────┘                                            │
└─────────────────────────────────────────────────────────────────┘
```

##### 语言配置表

```sql
-- 系统语言配置
CREATE TABLE sys_languages (
    code            VARCHAR(10) PRIMARY KEY,
    name            VARCHAR(50) NOT NULL,
    native_name     VARCHAR(50) NOT NULL,
    is_default      BOOLEAN DEFAULT FALSE,
    is_active      BOOLEAN DEFAULT TRUE,
    display_order  INTEGER DEFAULT 0,
    created_at      TIMESTAMP DEFAULT NOW()
);

INSERT INTO sys_languages (code, name, native_name, is_default, display_order) VALUES
('zh-CN', 'Chinese (Simplified)', '简体中文', TRUE, 1),
('en-US', 'English', 'English', FALSE, 2);

-- 用户语言偏好
ALTER TABLE users ADD COLUMN language VARCHAR(10) DEFAULT 'zh-CN';
```

##### 后端 API 多语言支持

- **用户偏好**：用户可设置个人语言偏好，登录时自动应用
- **错误消息**：API 错误返回多语言消息
- **通知模板**：告警通知支持多语言模板

```go
// 多语言错误消息
type ErrorMessage struct {
    Code    string `json:"code"`
    Message map[string]string `json:"message"`  // {"zh-CN": "错误", "en-US": "Error"}
}

var (
    ErrNotFound = ErrorMessage{
        Code: "NOT_FOUND",
        Message: map[string]string{
            "zh-CN": "资源不存在",
            "en-US": "Resource not found",
        },
    }
)
```

---

### 3.3 模块设计

#### 3.3.1 资产服务 (Asset Service)

```
职责：资产管理全生命周期

核心API:
- POST   /api/v1/assets              创建资产
- GET    /api/v1/assets              查询资产列表
- GET    /api/v1/assets/:id          查询资产详情
- PUT    /api/v1/assets/:id          更新资产
- DELETE /api/v1/assets/:id          删除资产
- POST   /api/v1/assets/batch        批量操作
- GET    /api/v1/assets/export       导出资产
- POST   /api/v1/assets/import       导入资产

数据模型:
- Asset (资产主表)
- AssetNetwork (网络信息)
- AssetHardware (硬件信息)
- AssetSoftware (软件信息)
- AssetLocation (物理位置)
- AssetMaintenance (维保信息)
- AssetHistory (变更历史)
```

#### 3.3.2 发现服务 (Discovery Service)

```
职责：网络自动发现、节点探测

核心功能:
- 网段扫描 (SNMP/ping/ARP)
- 设备识别 (指纹识别厂商、型号)
- 资产自动入库
- 变更检测

核心API:
- POST   /api/v1/discovery/tasks           创建发现任务
- GET    /api/v1/discovery/tasks           任务列表
- GET    /api/v1/discovery/tasks/:id       任务详情/进度
- POST   /api/v1/discovery/stop            停止任务
- GET    /api/v1/discovery/results         发现结果
```

#### 3.3.3 监控服务 (Monitoring Service)

```
职责：指标采集、阈值检查、实时数据

核心功能:
- 指标采集调度
- 阈值检查
- 实时数据推送 (WebSocket)
- 数据聚合

核心API:
- GET    /api/v1/metrics/:assetId          获取指标
- GET    /api/v1/metrics/history           历史数据
- POST   /api/v1/metrics/thresholds        设置阈值
- WS     /api/v1/ws/metrics               实时推送
```

#### 3.3.4 告警服务 (Alert Service)

```
职责：告警规则、告警触发、通知发送

核心功能:
- 告警规则管理
- 告警触发检查
- 通知渠道管理
- 告警升级/抑制
- 告警处理流程

核心API:
- POST   /api/v1/alerts/rules              创建规则
- GET    /api/v1/alerts/rules              规则列表
- PUT    /api/v1/alerts/rules/:id          更新规则
- GET    /api/v1/alerts                    告警列表
- PUT    /api/v1/alerts/:id/ack            确认告警
- PUT    /api/v1/alerts/:id/resolve        解决告警
- POST   /api/v1/alerts/channels           配置通知渠道
```

#### 3.3.5 拓扑服务 (Topology Service)

```
职责：网络拓扑发现、拓扑图管理

核心功能:
- 自动拓扑生成 (基于LLDP/CDP/ARP)
- 手动拓扑绘制
- 拓扑可视化
- 路径分析

核心API:
- GET    /api/v1/topology                  获取拓扑
- POST   /api/v1/topology/devices          添加设备
- PUT    /api/v1/topology/links            编辑链路
- GET    /api/v1/topology/path             路径分析
```

---

## 四、数据库设计

### 4.1 核心表结构

#### 4.1.1 资产表 (assets)

```sql
CREATE TABLE assets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_type      VARCHAR(50) NOT NULL,        -- server/switch/router/firewall/storage
    asset_name      VARCHAR(255) NOT NULL,
    asset_tag       VARCHAR(100),                -- 资产标签
    sn              VARCHAR(100),                -- 序列号
    brand           VARCHAR(100),                -- 品牌
    model           VARCHAR(100),                -- 型号
    
    -- 维保信息
    purchase_date   DATE,
    warranty_end    DATE,
    vendor          VARCHAR(255),                -- 供应商
    vendor_contact  VARCHAR(255),                -- 供应商联系方式
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active', -- active/offline/maintenance
    online_time     TIMESTAMP,
    offline_time    TIMESTAMP,
    
    -- 位置
    idc_id          UUID REFERENCES idc(id),
    rack_id         UUID REFERENCES racks(id),
    rack_position   VARCHAR(50),                 -- 如 "12U-18U"
    
    -- 其他
    tags            JSONB,
    custom_fields   JSONB,
    discovered_from VARCHAR(100),               -- 发现来源
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id),
    
    CONSTRAINT unique_asset UNIQUE (asset_tag, idc_id)
);

CREATE INDEX idx_assets_type ON assets(asset_type);
CREATE INDEX idx_assets_status ON assets(status);
CREATE INDEX idx_assets_idc ON assets(idc_id);
```

#### 4.1.2 机房表 (idc)

```sql
CREATE TABLE idc (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    code            VARCHAR(50) NOT NULL,
    province        VARCHAR(50),
    city            VARCHAR(50),
    address         TEXT,
    contact         VARCHAR(100),
    contact_phone   VARCHAR(50),
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);
```

#### 4.1.3 机柜表 (racks)

```sql
CREATE TABLE racks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idc_id          UUID NOT NULL REFERENCES idc(id),
    name            VARCHAR(50) NOT NULL,         -- 如 "A-01"
    total_u         INTEGER DEFAULT 42,           -- 总U位
    max_weight     INTEGER,                       -- 最大承重(kg)
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    
    CONSTRAINT unique_rack UNIQUE (idc_id, name)
);
```

#### 4.1.4 网络接口表 (asset_network)

```sql
CREATE TABLE asset_network (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    interface_name  VARCHAR(50) NOT NULL,          -- eth0/ens33/Gi1/0/1
    interface_type  VARCHAR(20),                   -- ethernet/loopback/virtual
    mac_address     VARCHAR(17),
    ipv4_address    INET,
    ipv4_netmask    INET,
    ipv6_address    INET,
    speed           INTEGER,                       -- Mbps
    duplex          VARCHAR(20),                   -- full/half
    status          VARCHAR(20),                   -- up/down/unknown
    
    -- 链路信息
    connected_to    UUID,                         -- 关联设备ID
    connected_port  VARCHAR(50),                  -- 对端端口
    
    -- 用途标记
    purpose         VARCHAR(50),                   -- management/storage/业务
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    
    CONSTRAINT unique_asset_interface UNIQUE (asset_id, interface_name)
);

CREATE INDEX idx_network_asset ON asset_network(asset_id);
CREATE INDEX idx_network_ipv4 ON asset_network(ipv4_address);
```

#### 4.1.5 硬件配置表 (asset_hardware)

```sql
CREATE TABLE asset_hardware (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    
    -- CPU
    cpu_model       VARCHAR(255),
    cpu_cores       INTEGER,
    cpu_physical    INTEGER,
    cpu_threads     INTEGER,
    cpu_speed       VARCHAR(50),
    
    -- 内存
    memory_total    BIGINT,                        -- MB
    memory_slots    INTEGER,
    memory_used     BIGINT,
    
    -- 硬盘
    disk_total      BIGINT,                        -- GB
    disk_type       VARCHAR(20),                   -- SSD/HDD
    disk_count      INTEGER,
    raid_config     VARCHAR(50),
    
    -- 电源
    power_count     INTEGER,
    power_status    VARCHAR(20),
    
    -- 风扇
    fan_count       INTEGER,
    fan_status      VARCHAR(20),
    
    -- 管理口
    mgmt_ip         INET,
    mgmt_type       VARCHAR(20),                   -- ipmi/ilo/idrac
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);
```

#### 4.1.6 软件信息表 (asset_software)

```sql
CREATE TABLE asset_software (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    
    software_type   VARCHAR(50) NOT NULL,          -- os/database/middleware/app
    software_name   VARCHAR(100) NOT NULL,
    version         VARCHAR(50),
    install_path    VARCHAR(255),
    port            INTEGER,
    process_name    VARCHAR(100),
    status          VARCHAR(20),
    
    -- 特定字段
    kernel_version  VARCHAR(50),                   -- OS
    ssh_version     VARCHAR(50),                   -- SSH
    openssl_version VARCHAR(50),                   -- OpenSSL
    db_version      VARCHAR(50),                   -- Database
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_software_asset ON asset_software(asset_id);
```

#### 4.1.7 监控指标表 (metrics) - TimescaleDB

```sql
-- 创建hypertable
CREATE TABLE metrics (
    time            TIMESTAMPTZ NOT NULL,
    asset_id        UUID NOT NULL,
    metric_name     VARCHAR(100) NOT NULL,
    metric_value    DOUBLE PRECISION,
    metric_unit     VARCHAR(20),
    tags            JSONB
);

SELECT create_hypertable('metrics', 'time');

CREATE INDEX idx_metrics_asset_time ON metrics(asset_id, time DESC);
CREATE INDEX idx_metrics_name_time ON metrics(metric_name, time DESC);

-- 常用指标视图
CREATE MATERIALIZED VIEW metrics_cpu AS
SELECT time_bucket('1 minute', time) AS bucket,
       asset_id,
       avg(metric_value) as value
FROM metrics WHERE metric_name = 'cpu_usage'
GROUP BY bucket, asset_id;

CREATE MATERIALIZED VIEW metrics_memory AS
SELECT time_bucket('1 minute', time) AS bucket,
       asset_id,
       avg(metric_value) as value
FROM metrics WHERE metric_name = 'memory_usage'
GROUP BY bucket, asset_id;
```

#### 4.1.8 告警表 (alerts)

```sql
CREATE TABLE alerts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID REFERENCES assets(id),
    alert_rule_id   UUID REFERENCES alert_rules(id),
    
    level           VARCHAR(20) NOT NULL,          -- info/warning/critical/emergency
    title           VARCHAR(255) NOT NULL,
    message         TEXT,
    metric_name     VARCHAR(100),
    metric_value    DOUBLE PRECISION,
    threshold       DOUBLE PRECISION,
    
    status          VARCHAR(20) DEFAULT 'firing', -- firing/acknowledged/resolved
    acknowledged_at TIMESTAMP,
    acknowledged_by UUID REFERENCES users(id),
    resolved_at     TIMESTAMP,
    resolved_by     UUID REFERENCES users(id),
    
    notified        BOOLEAN DEFAULT FALSE,
    notify_channels JSONB,                          -- 已发送的渠道
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_alerts_asset ON alerts(asset_id);
CREATE INDEX idx_alerts_status ON alerts(status);
CREATE INDEX idx_alerts_level ON alerts(level);
CREATE INDEX idx_alerts_created ON alerts(created_at DESC);
```

#### 4.1.9 告警规则表 (alert_rules)

```sql
CREATE TABLE alert_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    description     TEXT,
    
    -- 条件
    asset_type      VARCHAR(50),                   -- 资产类型过滤
    asset_ids       UUID[],                        -- 特定资产
    metric_name     VARCHAR(100) NOT NULL,
    operator        VARCHAR(10) NOT NULL,         -- >/</>=/<=/==/!=
    threshold       DOUBLE PRECISION NOT NULL,
    duration        INTEGER DEFAULT 0,              -- 持续秒数
    
    -- 级别
    level           VARCHAR(20) NOT NULL,
    
    -- 通知
    notify_channels JSONB,                          -- ["dingtalk", "email"]
    notify_users    UUID[],
    
    -- 状态
    enabled         BOOLEAN DEFAULT TRUE,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id)
);
```

#### 4.1.10 通知渠道表 (notify_channels)

```sql
CREATE TABLE notify_channels (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_type    VARCHAR(20) NOT NULL,          -- dingtalk/wechat/email/voice/webhook
    name            VARCHAR(100) NOT NULL,
    
    config          JSONB NOT NULL,                -- 渠道配置
    -- dingtalk: {webhook, secret, keyword}
    -- wechat: {webhook, agentid}
    -- email: {smtp_host, smtp_port, username, password, from, to}
    -- voice: {provider, api_key, phone}
    -- webhook: {url, method, headers}
    
    enabled         BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);
```

#### 4.1.11 拓扑节点表 (topology_nodes)

```sql
CREATE TABLE topology_nodes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID REFERENCES assets(id),     -- 关联资产
    node_type       VARCHAR(50) NOT NULL,          -- device/endpoint/group
    label           VARCHAR(100),
    x               FLOAT,
    y               FLOAT,
    style           JSONB,                         -- 样式配置
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);
```

#### 4.1.12 拓扑链路表 (topology_links)

```sql
CREATE TABLE topology_links (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_node_id  UUID NOT NULL REFERENCES topology_nodes(id),
    target_node_id  UUID NOT NULL REFERENCES topology_nodes(id),
    
    link_type       VARCHAR(50),                   -- network/metal/virtual
    bandwidth       VARCHAR(50),
    label           VARCHAR(100),
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);
```

### 4.2 数据库高可用设计

```
┌─────────────────────────────────────────────────────────┐
│                    应用服务 (多副本)                     │
└─────────────────────┬───────────────────────────────────┘
                      │
         ┌────────────┼────────────┐
         │            │            │
         ▼            ▼            ▼
    ┌────────┐  ┌────────┐  ┌────────┐
    │PostgreSQL│ │PostgreSQL│ │PostgreSQL│
    │ 主节点  │◄─┼─ 备节点 │◄─┼─ 备节点 │
    └────────┘  └────────┘  └────────┘
         │
         │ 同步/流复制
         │
         ▼
    ┌──────────────────┐
    │    TimescaleDB   │  (时序数据)
    │  (可分区/分片)   │
    └──────────────────┘
```

- **PostgreSQL**: 主从流复制，支持读写分离
- **TimescaleDB**: 时序数据压缩、压缩保留策略
- **Redis**: 主从/Sentinel模式
- **MongoDB**: 副本集

---

## 五、API设计

### 5.1 RESTful API规范

```
Base URL: /api/v1

通用响应格式:
{
  "code": 0,           // 0=成功, 非0=错误
  "message": "success",
  "data": { ... },
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 100
  }
}

错误响应:
{
  "code": 400,
  "message": "Invalid parameter",
  "detail": { ... }
}
```

### 5.2 核心API列表

| 模块 | 方法 | 路径 | 说明 |
|------|------|------|------|
| 资产 | GET | /assets | 资产列表 |
| 资产 | POST | /assets | 创建资产 |
| 资产 | GET | /assets/:id | 资产详情 |
| 资产 | PUT | /assets/:id | 更新资产 |
| 资产 | DELETE | /assets/:id | 删除资产 |
| 资产 | GET | /assets/:id/networks | 网络接口列表 |
| 资产 | POST | /assets/:id/networks | 添加网络接口 |
| 资产 | GET | /assets/:id/software | 软件列表 |
| 资产 | GET | /assets/export | 导出资产 |
| 资产 | POST | /assets/import | 导入资产 |
| 发现 | POST | /discovery/tasks | 创建发现任务 |
| 发现 | GET | /discovery/tasks | 发现任务列表 |
| 发现 | GET | /discovery/tasks/:id | 任务详情 |
| 发现 | POST | /discovery/tasks/:id/stop | 停止任务 |
| 监控 | GET | /metrics/:assetId | 实时指标 |
| 监控 | GET | /metrics/history | 历史数据 |
| 监控 | POST | /metrics/thresholds | 设置阈值 |
| 告警 | GET | /alerts | 告警列表 |
| 告警 | PUT | /alerts/:id/ack | 确认告警 |
| 告警 | PUT | /alerts/:id/resolve | 解决告警 |
| 告警 | GET | /alerts/rules | 告警规则 |
| 告警 | POST | /alerts/rules | 创建规则 |
| 拓扑 | GET | /topology | 获取拓扑 |
| 拓扑 | POST | /topology/nodes | 添加节点 |
| 拓扑 | PUT | /topology/links | 更新链路 |

---

## 六、采集器设计

### 6.1 SNMP采集器

```go
// 核心采集逻辑
type SNMPCollector struct {
    client *gosnmp.GoSNMP
}

### 6.1 SNMP采集器

```go
// 核心采集逻辑
type SNMPCollector struct {
    client *gosnmp.GoSNMP
}

func (c *SNMPCollector) Collect(target string, oid string) (interface{}, error) {
    result, err := c.client.Get([]string{oid})
    if err != nil {
        return nil, err
    }
    return result.Variables[0].Value, nil
}

// 常用OID
var CommonOIDs = map[string]string{
    // 系统
    "sysDescr":     "1.3.6.1.2.1.1.1.0",
    "sysUpTime":    "1.3.6.1.2.1.1.3.0",
    "sysContact":   "1.3.6.1.2.1.1.4.0",
    "sysName":      "1.3.6.1.2.1.1.5.0",
    "sysLocation":  "1.3.6.1.2.1.1.6.0",
    
    // CPU (CpqHe)
    "cpuUtil":      "1.3.6.1.4.1.232.11.2.3.1.1.1.0",
    
    // 内存
    "memTotal":     "1.3.6.1.2.1.25.2.2.0",
    
    // 端口状态
    "ifDescr":      "1.3.6.1.2.1.2.2.1.2",
    "ifSpeed":      "1.3.6.1.2.1.2.2.1.5",
    "ifInOctets":   "1.3.6.1.2.1.2.2.1.10",
    "ifOutOctets":  "1.3.6.1.2.1.2.2.1.16",
    "ifOperStatus": "1.3.6.1.2.1.2.2.1.8",
}
```

#### 6.1.1 支持的SNMP版本

| 版本 | 安全级别 | 适用场景 |
|------|----------|----------|
| SNMP v1 | 团体名(Community) | 内部网络、测试环境 |
| SNMP v2c | 团体名(Community) | 广泛兼容 |
| SNMP v3 | USM(用户安全模型) | 生产环境、敏感网络 |

#### 6.1.2 设备指纹识别

通过SNMP获取sysDescr、sysObjectID等识别厂商和型号：

```
Cisco:      sysObjectID = .1.3.6.1.4.1.9.1.*
Huawei:     sysObjectID = .1.3.6.1.4.1.2011.*
H3C:        sysObjectID = .1.3.6.1.4.1.25506.*
Dell:       sysObjectID = .1.3.6.1.4.1.674.*
HP:         sysObjectID = .1.3.6.1.4.1.11.*
```

### 6.2 Agent设计

#### 6.2.1 Agent架构

```
┌─────────────────────────────────────┐
│           Agent (Go)                │
├─────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  │
│  │  指标采集   │  │  日志收集    │  │
│  │  (Exporter)│  │  (Filebeat) │  │
│  └─────────────┘  └─────────────┘  │
│              │                      │
│              ▼                      │
│  ┌─────────────────────────────┐   │
│  │      本地缓存 & 压缩        │   │
│  └─────────────────────────────┘   │
│              │                      │
│              ▼                      │
│  ┌─────────────────────────────┐   │
│  │    HTTP/HTTPS 上报          │   │
│  └─────────────────────────────┘   │
└─────────────────────────────────────┘
```

#### 6.2.2 采集指标

| 类别 | 指标 | 说明 |
|------|------|------|
| 系统 | system.cpu.usage | CPU使用率 |
| 系统 | system.memory.usage | 内存使用率 |
| 系统 | system.disk.usage | 磁盘使用率 |
| 系统 | system.load.1/5/15 | 负载 |
| 网络 | network.rx_bytes/tx_bytes | 网卡流量 |
| 网络 | network.conn_count | 连接数 |
| 进程 | process.cpu/memory | 进程资源 |
| 端口 | port.listen/established | 端口状态 |

#### 6.2.3 心跳与离线

- Agent每60秒上报心跳
- 超过3分钟无心跳视为离线
- 离线期间数据本地缓存，恢复后补传

### 6.3 采集任务调度

```go
// 分布式任务调度
type CollectorScheduler struct {
    redis     *redis.Client
    workers   int
}

func (s *CollectorScheduler) Schedule(task *CollectTask) {
    // 按资产ID hash分配到特定worker
    workerID := hash(task.AssetID) % s.workers
    
    // 写入任务队列
    s.redis.LPush(fmt.Sprintf("queue:collector:%d", workerID), task)
}
```

---

## 七、告警通知设计

### 7.1 通知渠道实现

#### 7.1.1 钉钉通知

```go
type DingTalkNotifier struct {
    webhook  string
    secret   string
}

func (n *DingTalkNotifier) Send alert Alert) error {
    // 生成签名
    timestamp := time.Now().UnixMilli()
    sign := n.generateSign(timestamp)
    
    // 构造消息
    msg := map[string]interface{}{
        "msgtype": "markdown",
        "markdown": map[string]string{
            "title": fmt.Sprintf("[%s] %s", alert.Level, alert.Title),
            "text":  alert.Message,
        },
    }
    
    // 发送请求
    resp, err := http.PostForm(n.webhook, url.Values{
        "timestamp": {strconv.FormatInt(timestamp, 10)},
        "sign":      {sign},
    })
    // ...
}
```

#### 7.1.2 邮件通知

```go
type EmailNotifier struct {
    smtpHost string
    smtpPort int
    username string
    password string
    from     string
}

func (n *EmailNotifier) Send(alert Alert) error {
    msg := gomail.NewMessage()
    msg.SetHeader("From", n.from)
    msg.SetHeader("To", alert.NotifyEmails...)
    msg.SetHeader("Subject", fmt.Sprintf("[%s] %s", alert.Level, alert.Title))
    msg.SetBody("text/html", n.formatHTML(alert))
    
    dialer := gomail.NewDialer(n.smtpHost, n.smtpPort, n.username, n.password)
    return dialer.DialAndSend(msg)
}
```

#### 7.1.3 语音通知

```go
type VoiceNotifier struct {
    provider string      // aliyun/tencent/custom
    apiKey   string
}

func (n *VoiceNotifier) Send(alert Alert) error {
    // 最高级别告警触发语音
    if alert.Level != "emergency" {
        return nil
    }
    
    // 调用TTS或呼叫API
    switch n.provider {
    case "aliyun":
        return n.aliyunCall(alert)
    case "tencent":
        return n.tencentCall(alert)
    }
    // ...
}
```

### 7.2 告警处理流程

```
                    ┌──────────────┐
                    │  指标采集    │
                    └──────┬───────┘
                           │
                           ▼
                    ┌──────────────┐
                    │  阈值检查    │
                    └──────┬───────┘
                           │
              ┌────────────┴────────────┐
              │                         │
              ▼                         ▼
        ┌──────────┐            ┌──────────┐
        │  未触发   │            │  触发    │
        └──────────┘            └─────┬────┘
                                      │
                                      ▼
                               ┌──────────────┐
                               │  创建告警    │
                               └──────┬───────┘
                                      │
                                      ▼
                               ┌──────────────┐
                               │  通知调度    │
                               └──────┬───────┘
                                      │
              ┌───────────────────────┼───────────────────────┐
              │                       │                       │
              ▼                       ▼                       ▼
        ┌──────────┐          ┌──────────┐          ┌──────────┐
        │  钉钉    │          │  邮件    │          │  语音    │
        └──────────┘          └──────────┘          └──────────┘
                                      │
                                      ▼
                               ┌──────────────┐
                               │  等待确认    │
                               └──────┬───────┘
                                      │
              ┌───────────────────────┼───────────────────────┐
              │                       │                       │
              ▼                       ▼                       ▼
        ┌──────────┐          ┌──────────┐          ┌──────────┐
        │  已确认  │          │  已解决   │          │  自动恢复 │
        └──────────┘          └──────────┘          └──────────┘
```

---

## 八、前端设计

### 8.1 页面结构

```
/
├── 登录/认证
├── 主布局
│   ├── 顶部导航栏
│   ├── 侧边栏菜单
│   └── 主内容区
├── 仪表盘/
│   ├── 总览卡片
│   ├── 告警统计
│   └── 资产分布
├── 资产管理/
│   ├── 资产列表
│   ├── 资产详情
│   │   ├── 基本信息
│   │   ├── 网络信息
│   │   ├── 硬件配置
│   │   ├── 软件信息
│   │   └── 监控指标
│   ├── 资产录入
│   └── 批量导入
├── 机柜视图/
│   ├── 机房列表
│   ├── 机柜3D视图
│   └── 设备面板
├── 拓扑管理/
│   ├── 拓扑总览
│   ├── 自动拓扑
│   └── 手动绘制
├── 监控中心/
│   ├── 实时监控
│   ├── 指标图表
│   └── 历史趋势
├── 告警中心/
│   ├── 告警列表
│   ├── 告警规则
│   └── 通知渠道
├── 发现任务/
│   ├── 任务列表
│   └── 任务详情
├── 报表/
│   ├── 资产报表
│   └── 可用性报表
└── 系统设置/
    ├── 用户管理
    ├── 角色权限
    └── 系统配置
```

### 8.2 关键技术实现

#### 8.2.1 机柜可视化

```tsx
// React + D3.js 机柜组件
const RackView: React.FC<{ rackId: string }> = ({ rackId }) => {
  const { data: rack } = useQuery(GET_RACK, { variables: { rackId } });
  
  return (
    <div className="rack-container">
      <svg viewBox="0 0 600 1200">
        {/* 机柜框 */}
        <rect x="50" y="50" width="500" height="1100" className="rack-frame" />
        
        {/* U位 */}
        {_.range(1, 43).map(u => (
          <g key={u} transform={`translate(60, ${1140 - u * 26})`}>
            <rect width="480" height="24" className="u-slot" />
            
            {/* 设备 */}
            {rack.devices.filter(d => d.startU === u).map(device => (
              <RackDevice key={device.id} device={device} />
            ))}
          </g>
        ))}
      </svg>
    </div>
  );
};
```

#### 8.2.2 拓扑图

```tsx
// G6 拓扑图
import { Graph } from '@antv/g6';

const TopologyGraph: React.FC = () => {
  const graph = useRef<Graph>();
  
  useEffect(() => {
    graph.current = new Graph({
      container: 'topology-container',
      modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
      layout: {
        type: 'dagre',
        rankdir: 'LR',
        nodesep: 50,
        ranksep: 100,
      },
      defaultNode: { type: 'rect', size: [120, 40] },
      defaultEdge: { type: 'cubic-horizontal' },
    });
    
    graph.current.data(topologyData);
    graph.current.render();
  }, []);
  
  return <div id="topology-container" />;
};
```

#### 8.2.3 实时数据推送

```tsx
// WebSocket 实时监控
const useRealtimeMetrics = (assetId: string) => {
  const [metrics, setMetrics] = useState<Metric[]>([]);
  
  useEffect(() => {
    const ws = new WebSocket(`wss://api.example.com/ws/metrics/${assetId}`);
    
    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      setMetrics(prev => [...prev.slice(-60), data]); // 保留60个点
    };
    
    return () => ws.close();
  }, [assetId]);
  
  return metrics;
};
```

---

## 九、部署架构

### 9.1 单机部署

```
┌─────────────────────────────────────────────┐
│                 最小部署                      │
├─────────────────────────────────────────────┤
│  ┌─────────────────────────────────────┐    │
│  │           Docker Compose            │    │
│  ├─────────────────────────────────────┤    │
│  │  frontend    (React)               │    │
│  │  api-gateway  (Nginx)              │    │
│  │  asset-svc   (Go)                  │    │
│  │  monitor-svc (Go)                  │    │
│  │  alert-svc   (Go)                  │    │
│  │  collector   (Go)                   │    │
│  │  postgres    (DB)                  │    │
│  │  timescale   (TSDB)                │    │
│  │  redis       (Cache/Queue)        │    │
│  │  mongodb     (Logs)               │    │
│  └─────────────────────────────────────┘    │
└─────────────────────────────────────────────┘
```

### 9.2 生产环境部署

```
                              ┌─────────────────┐
                              │   负载均衡器     │
                              │   (Nginx/HAProxy)│
                              └────────┬────────┘
                                       │
              ┌────────────────────────┼────────────────────────┐
              │                        │                        │
              ▼                        ▼                        ▼
    ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
    │   API Server 1   │    │   API Server 2   │    │   API Server 3  │
    │   (Go + Gin)    │    │   (Go + Gin)    │    │   (Go + Gin)    │
    └────────┬────────┘    └────────┬────────┘    └────────┬────────┘
             │                       │                       │
             └───────────────────────┼───────────────────────┘
                                     │
        ┌────────────────────────────┼────────────────────────────┐
        │                            │                            │
        ▼                            ▼                            ▼
┌───────────────┐          ┌───────────────┐          ┌───────────────┐
│   PostgreSQL  │          │  TimescaleDB  │          │     Redis     │
│   主节点(写)  │◄─────────│   主节点     │◄─────────│   主节点      │
└───────────────┘          └───────────────┘          └───────────────┘
        │                            │                            │
        │                            │                            │
        ▼                            │                            ▼
┌───────────────┐                     │                  ┌───────────────┐
│  PostgreSQL   │                     │                  │    Redis      │
│  从节点(读)  │                     │                  │    从节点     │
└───────────────┘                     │                  └───────────────┘
                                     ▼
                            ┌───────────────┐
                            │  TimescaleDB  │
                            │  从节点(读)  │
                            └───────────────┘

采集器:
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│ Collector-1 │  │ Collector-2 │  │ Collector-N │
│ (SNMP/SSH) │  │ (SNMP/SSH)  │  │ (Agent)    │
└─────────────┘  └─────────────┘  └─────────────┘
```

### 9.3 Docker Compose 配置

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: monitor
      POSTGRES_USER: monitor
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - monitor_net

  timescale:
    image: timescale/timescaledb:latest-pg15
    environment:
      POSTGRES_DB: metrics
      POSTGRES_USER: metrics
      POSTGRES_PASSWORD: ${METRICS_PASSWORD}
    volumes:
      - timescale_data:/var/lib/postgresql/data
    networks:
      - monitor_net

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes
    volumes:
      - redis_data:/data
    networks:
      - monitor_net

  api:
    build: ./api
    ports:
      - "8080:8080"
    environment:
      DB_HOST: postgres
      REDIS_HOST: redis
      TIMESERIES_HOST: timescale
    depends_on:
      - postgres
      - redis
      - timescale
    networks:
      - monitor_net

  collector:
    build: ./collector
    environment:
      API_URL: http://api:8080
      REDIS_HOST: redis
    networks:
      - monitor_net

  frontend:
    build: ./frontend
    ports:
      - "80:80"
    depends_on:
      - api
    networks:
      - monitor_net

networks:
  monitor_net:
    driver: bridge

volumes:
  postgres_data:
  timescale_data:
  redis_data:
```

---

## 十、AI辅助分析模块

### 10.1 模块概述

AI辅助分析模块为运维团队提供智能化能力，包括自然语言查询、向量化知识库检索、异常检测、根因分析等。

```
┌─────────────────────────────────────────────────────────────────────┐
│                      AI分析服务架构                                   │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────────┐        │
│   │  用户交互层  │    │  多模态输入  │    │  对话历史    │        │
│   │  (Chat UI)  │    │ (文本/截图) │    │  管理       │        │
│   └──────┬───────┘    └──────┬───────┘    └──────┬───────┘        │
│          │                   │                   │                  │
│          └───────────────────┼───────────────────┘                  │
│                              │                                       │
│                              ▼                                       │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │                    AI网关层                                  │   │
│   │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐         │   │
│   │  │  意图识别   │ │  上下文管理 │ │  路由分发   │         │   │
│   │  └─────────────┘ └─────────────┘ └─────────────┘         │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                              │                                       │
│                              ▼                                       │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │                    LLM编排层                                  │   │
│   │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐         │   │
│   │  │  多LLM适配  │ │  Prompt工程 │ │  输出解析   │         │   │
│   │  └─────────────┘ └─────────────┘ └─────────────┘         │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                              │                                       │
│                              ▼                                       │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │                    向量化知识库                              │   │
│   │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐         │   │
│   │  │  文档处理   │ │  向量存储   │ │  相似检索   │         │   │
│   │  │  (Embedding)│ │  (Milvus)  │ │  (VectorDB) │         │   │
│   │  └─────────────┘ └─────────────┘ └─────────────┘         │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                              │                                       │
│                              ▼                                       │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │                    LLM提供商                                  │   │
│   │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │   │
│   │  │  OpenAI  │ │  Claude  │ │  Ollama  │ │  本地模型 │    │   │
│   │  └──────────┘ └──────────┘ └──────────┘ └──────────┘    │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 10.2 多LLM支持

#### 10.2.1 LLM适配器设计

```go
// LLM接口定义
type LLMProvider interface {
    Name() string
    Chat(request ChatRequest) (ChatResponse, error)
    Embedding(texts []string) ([]float32, error)
    SupportsVision() bool
}

// 多提供商实现
type OpenAIProvider struct {
    apiKey   string
    model    string
}

type OllamaProvider struct {
    endpoint string
    model    string
}

type ClaudeProvider struct {
    apiKey   string
    model    string
}

// 使用示例
func UseLLM(providerName string, prompt string) string {
    provider := GetProvider(providerName)
    return provider.Chat(ChatRequest{Prompt: prompt})
}
```

#### 10.2.2 支持的LLM提供商

| 提供商 | 类型 | 模型示例 | 特点 |
|--------|------|----------|------|
| **OpenAI** | 云API | GPT-4o, GPT-4o-mini | 能力强，成本高，支持第三方兼容 |
| **Anthropic Claude** | 云API | Claude 3.5 Sonnet | 长文本优秀 |
| **Google Gemini** | 云API | Gemini 1.5 Pro | 多模态强 |
| **Azure OpenAI** | 私有云 | GPT-4, GPT-3.5 | 企业合规 |
| **Ollama** | 本地 | Llama3, Mistral, Qwen | 完全私有，低延迟 |
| **本地量化模型** | 本地 | ChatGLM3, Baichuan | 国产化，支持 |
| **智谱AI** | 云API | GLM-4 | 中文优化 |
| **Minimax** | 云API | ABAB系列 | 高性价比，快速响应 |
| **DeepSeek** | 云API | DeepSeek-V2, DeepSeek-chat | 推理能力强，开源友好 |

#### 10.2.2.1 OpenAI兼容配置

支持配置第三方OpenAI兼容的API服务（如硅基流动、OneAPI等）：

```go
// OpenAI兼容提供商配置
type OpenAICompatibleProvider struct {
    // 基础配置
    Name      string `config:"name"`       // 提供商名称
    BaseURL   string `config:"base_url"`  // API基础URL
    APIKey    string `config:"api_key"`
    Model     string `config:"model"`
    
    // 兼容性配置
    Timeout   time.Duration `config:"timeout"`
    MaxRetries int `config:"max_retries"`
    
    // HTTP客户端配置
    HTTPClient *http.Client
}

// 配置示例
providers:
  # 官方OpenAI
  - name: "openai-official"
    base_url: "https://api.openai.com/v1"
    model: "gpt-4o"
    
  # 硅基流动 (OpenAI兼容)
  - name: "siliconflow"
    base_url: "https://api.siliconflow.cn/v1"
    model: "deepseek-ai/DeepSeek-V2"
    
  # OneAPI聚合平台
  - name: "oneapi"
    base_url: "http://oneapi.company.com/v1"
    model: "gpt-4"
    
  # 智谱AI
  - name: "zhipu"
    base_url: "https://open.bigmodel.cn/api/paas/v4"
    model: "glm-4"
```

#### 10.2.3 模型路由策略

```go
// 智能路由选择
type ModelRouter struct {
    rules []RouterRule
}

type RouterRule struct {
    Intent       string   // 查询意图
    Models       []string // 可用模型
    MaxTokens    int
    PreferLocal  bool     // 优先本地
    FallbackModel string  // 备用模型
}

// 默认策略
var DefaultRouting = ModelRouter{
    Rules: []RouterRule{
        {
            Intent: "simple_query",
            Models: []string{"ollama:qwen2:7b", "gpt-3.5-turbo"},
            PreferLocal: true,
        },
        {
            Intent: "complex_analysis",
            Models: []string{"gpt-4o", "claude-3-5-sonnet"},
            PreferLocal: false,
        },
        {
            Intent: "vision_analysis",
            Models: []string{"gpt-4o", "gemini-1.5-pro"},
            PreferLocal: false,
        },
    },
}
```

### 10.3 向量化知识库

#### 10.3.1 知识库类型

| 类型 | 内容 | 用途 |
|------|------|------|
| **运维知识库** | 操作手册、故障处理流程、配置指南 | 智能问答 |
| **资产知识库** | 设备型号规格、厂商文档、维保信息 | 资产咨询 |
| **告警知识库** | 历史告警处理记录、解决方案 | 告警推荐 |
| **拓扑知识库** | 网络架构文档、变更记录 | 变更分析 |
| **日志知识库** | 错误日志样本、排查指南 | 日志分析 |

#### 10.3.2 向量化和检索流程

```
                    ┌─────────────────┐
                    │  原始文档上传   │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  文档解析      │
                    │  (PDF/MD/TXT)  │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
        ┌─────────┐   ┌─────────┐   ┌─────────┐
        │ 文本分块 │   │  表格   │   │  图片   │
        │  (Chunk)│   │  提取   │   │  OCR   │
        └────┬────┘   └────┬────┘   └────┬────┘
             │             │             │
             └─────────────┼─────────────┘
                           │
                           ▼
                    ┌─────────────────┐
                    │  Embedding模型  │
                    │  (BGE/M3E)     │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  向量存储       │
                    │  (Milvus/Qdrant)│
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │   用户查询      │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  查询向量化     │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  相似度检索     │
                    │  (Top-K)       │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  上下文拼接     │
                    │  + Prompt构建   │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │   LLM生成答案   │
                    └─────────────────┘
```

#### 10.3.3 技术栈

| 组件 | 选型 | 理由 |
|------|------|------|
| **Embedding** | BGE-M3, M3E | 中文效果好，开源 |
| **向量数据库(轻量)** | **Qdrant** / ChromaDB | **Rust/C++编写，性能高，资源占用低** |
| **向量数据库(生产)** | Milvus | 大规模分布式场景 |
| **文档处理** | LangChain, Unstructured | 丰富的解析能力 |
| **检索增强** | RAG (Retrieval-Augmented Generation) | 提升准确性 |

#### 10.3.3.1 向量数据库选型对比

| 方案 | 轻量级 | 多模态 | 易维护 | 持久化 | 推荐场景 |
|------|--------|--------|--------|--------|--------|----------|
| **Weaviate** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | 原生持久化 | **多模态推荐** |
| **Qdrant** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | 原生持久化 | 生产平衡 |
| **ChromaDB** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 单文件 | 快速原型 |
| **Milvus** | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | K8s较重 | 大规模企业 |

#### 10.3.3.2 Weaviate配置示例（多模态推荐）

```yaml
# weaviate-config.yaml
# 多模态向量数据库配置

weaviate:
  # 服务配置
  host: "0.0.0.0"
  http_port: 8080
  grpc_port: 50051
  
  # 认证配置
  authentication:
    anonymous_access_enabled: false
    api_key: ${WEAVIATE_API_KEY}
    
  # 存储配置
  persistence:
    data_path: "/data/weaviate"
    backup_path: "/data/weaviate/backups"
    
  # 模块配置（核心优势：多模态）
  modules:
    # 文本向量化
    text2vec-transformers:
      enabled: true
      model: "sentence-transformers/all-MiniLM-L6-v2"
      
    # 图像向量化（CLIP）
    img2vec-neural:
      enabled: true
      model: "clip"
      
  # 集合配置
  collections:
    - name: "kb_embeddings"
      description: "运维知识库"
      vectorizer: "text2vec-transformers"
      
    - name: "device_images"
      description: "设备图片库"
      vectorizer: "img2vec-neural"
```

#### 10.3.3.3 Weaviate多模态操作示例

```go
// Weaviate多模态向量服务
type WeaviateService struct {
    client *weaviate.Client
}

// 上传图片（自动向量化 - CLIP）
func (s *WeaviateService) AddImage(collection string, imagePath string, meta map[string]interface{}) error {
    imgData, _ := os.ReadFile(imagePath)
    base64Img := base64.StdEncoding.EncodeToString(imgData)
    
    _, err := s.client.Data().Creator().
        WithClassName(collection).
        WithProperties(mergeMaps(map[string]interface{}{
            "image": base64Img,
            "type": "image",
        }, meta)).
        Do(context.Background())
    return err
}

// 混合搜索（文本+图像）
func (s *WeaviateService) HybridSearch(collection string, query string, imagePath string, limit int) ([]SearchResult, error) {
    results, err := s.client.GraphQL().Get().
        WithClassName(collection).
        WithNearText(s.client.GraphQL().NearTextParamBuilder().
            WithConcepts([]string{query}).
            Build()).
        WithLimit(limit).
        WithFields("content", "type", "_additional {certainty score}").
        Do(context.Background())
    return results, err
}
```

#### 10.3.3.4 Qdrant配置示例

```yaml
# qdrant-config.yaml
# 轻量级向量数据库配置

qdrant:
  # 服务配置
  host: "0.0.0.0"
  port: 6333
  grpc_port: 6334
  
  # 存储配置
  storage:
    storage_path: "/data/qdrant"
    wal_capacity_mb: 64
    flush_interval_sec: 15
    
  # 性能配置
  performance:
    max_optimize_index_size: 1000000
    optimizers_threshold_delete_ratio: 0.2
    default_segment_number: 2
    
  # 集合配置
  collections:
    - name: "kb_embeddings"
      vector_size: 1024
      distance: "Cosine"
      on_disk: true
      
    - name: "device_embeddings"
      vector_size: 768
      distance: "Dot"
      on_disk: true
```

#### 10.3.3.3 ChromaDB配置示例（轻量原型）

```yaml
# chromadb-config.yaml
# 极速部署配置

chromadb:
  # 存储配置
  persist_directory: "/data/chromadb"
  
  # 集合配置
  collections:
    - name: "kb_embeddings"
      metadata:
        hnsw:
          space: "cosine"
          ef: 100
          
    - name: "device_embeddings"
      metadata:
        hnsw:
          space: "dot"
          ef: 200
```

#### 10.3.3.4 向量数据库操作示例

```go
// Qdrant向量数据库服务
type VectorDBService struct {
    client *qdrant.Client
}

func (s *VectorDBService) AddDocument(collection string, doc *Document) error {
    // 1. 文档分块
    chunks := s.SplitDocument(doc.Content)
    
    // 2. 生成向量
    embeddings := s.GenerateEmbeddings(chunks)
    
    // 3. 存储到Qdrant
    points := make([]*qdrant.Point, len(chunks))
    for i, chunk := range chunks {
        points[i] = &qdrant.Point{
            Id:      uuid.New().String(),
            Vector:  embeddings[i],
            Payload: map[string]any{
                "doc_id":    doc.ID,
                "chunk_id": i,
                "content":   chunk,
                "metadata":  doc.Metadata,
            },
        }
    }
    
    s.client.Collection(collection).Upsert(points)
    return nil
}

func (s *VectorDBService) Search(collection string, query string, topK int) ([]SearchResult, error) {
    // 1. 查询向量化
    queryVec := s.GenerateEmbedding(query)
    
    // 2. 相似度搜索
    results, err := s.client.Collection(collection).Search(
        queryVec,
        topK,
        qdrant.WithScore(true),
    )
    
    return results, err
}
```

### 10.4 AI功能场景

#### 10.4.1 智能告警分析

```
用户输入: "Server-01的CPU告警，可能是什么原因？"

AI分析流程:
1. 检索告警知识库 → 获取类似告警处理记录
2. 获取监控数据 → CPU历史趋势、关联指标
3. 结合资产信息 → 设备型号、配置信息
4. 生成分析报告 → 根因分析、建议操作

输出:
"根据历史数据和相似案例，Server-01 CPU告警可能原因:
1. 业务高峰期流量突增 (14:00-15:00 流量上涨40%)
2. 建议检查:
   - 当前运行进程 (top命令)
   - 网络连接数 (ESTABLISHED连接数异常)
   - 应用日志 (查看是否有异常请求)"
```

#### 10.4.2 自然语言查询

```
用户输入: "帮我查一下过去24小时内存使用率超过80%的服务器"

AI处理:
1. 意图识别 → 查询意图
2. 实体提取 → 时间(24h), 指标(内存), 阈值(80%)
3. 转换为SQL查询
4. 执行查询，返回结果

SELECT asset_name, max(metric_value) as peak_usage
FROM metrics
WHERE metric_name = 'memory_usage'
  AND time > NOW() - INTERVAL '24 hours'
  AND metric_value > 80
GROUP BY asset_id, asset_name
```

#### 10.4.3 多模态分析

| 能力 | 说明 |
|------|------|
| **截图分析** | 上传监控截图，AI识别问题 |
| **拓扑图分析** | 分析网络拓扑，识别风险点 |
| **日志图片** | 识别日志中的错误模式 |
| **仪表盘解读** | 自动解读监控仪表盘 |

### 10.5 数据库设计 - AI模块

```sql
-- 知识库文档表
CREATE TABLE kb_documents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title           VARCHAR(255) NOT NULL,
    content         TEXT NOT NULL,
    doc_type        VARCHAR(50) NOT NULL,          -- ops/asset/alert/topology
    tags            JSONB,
    vector_id       VARCHAR(100),                  -- Milvus ID
    
    status          VARCHAR(20) DEFAULT 'pending',  -- pending/indexed/error
    chunk_count     INTEGER DEFAULT 0,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id)
);

-- 对话历史表
CREATE TABLE ai_conversations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    title           VARCHAR(255),
    summary         TEXT,
    
    -- 上下文窗口
    context_tokens   INTEGER DEFAULT 0,
    max_tokens      INTEGER DEFAULT 4000,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 对话消息表
CREATE TABLE ai_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES ai_conversations(id),
    role            VARCHAR(20) NOT NULL,           -- user/assistant/system
    content         TEXT NOT NULL,
    message_type    VARCHAR(50),                    -- text/vision/query
    
    -- 引用知识
    referenced_docs JSONB,                          -- 引用的文档ID
    
    -- 性能数据
    latency_ms      INTEGER,
    tokens_used     INTEGER,
    llm_provider    VARCHAR(50),
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- LLM提供商配置表
CREATE TABLE llm_providers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(50) NOT NULL,           -- openai/ollama/claude
    display_name    VARCHAR(100),
    
    -- 配置
    api_endpoint    VARCHAR(255),
    api_key         VARCHAR(255),                   -- 加密存储
    model           VARCHAR(100) DEFAULT 'gpt-3.5-turbo',
    
    -- 能力
    supports_vision BOOLEAN DEFAULT FALSE,
    max_tokens      INTEGER DEFAULT 4096,
    embedding_model VARCHAR(100),
    
    -- 状态
    enabled         BOOLEAN DEFAULT TRUE,
    is_default      BOOLEAN DEFAULT FALSE,
    priority        INTEGER DEFAULT 100,            -- 优先级
    
    -- 限流
    rate_limit      INTEGER,                        -- QPM
    monthly_limit   BIGINT,                         -- 预算限制
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);
```

---

## 十一、用户权限管理

### 11.1 RBAC权限模型

```
┌─────────────────────────────────────────────────────────────────────┐
│                      RBAC权限架构                                    │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌──────────┐     ┌──────────┐     ┌──────────┐                    │
│   │   用户   │────►│   角色   │────►│   权限   │                    │
│   └──────────┘     └──────────┘     └──────────┘                    │
│        │                │                │                           │
│        │                │                │                           │
│   ┌────┴────┐      ┌────┴────┐      ┌────┴────┐                     │
│   │ 用户组  │      │ 角色组  │      │ 权限组  │                     │
│   └─────────┘      └─────────┘      └─────────┘                     │
│                                                                      │
│   ┌─────────────────────────────────────────────────────────────┐ │
│   │                    权限继承链                                  │ │
│   │   用户 → 用户组 → 角色 → 角色组 → 权限                       │ │
│   └─────────────────────────────────────────────────────────────┘ │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 11.2 权限粒度

#### 11.2.1 功能权限

| 模块 | 操作 | 权限代码 |
|------|------|----------|
| 资产 | 查看 | asset:read |
| 资产 | 创建 | asset:create |
| 资产 | 编辑 | asset:update |
| 资产 | 删除 | asset:delete |
| 资产 | 导出 | asset:export |
| 监控 | 查看 | monitor:read |
| 监控 | 配置 | monitor:config |
| 告警 | 查看 | alert:read |
| 告警 | 处理 | alert:handle |
| 告警 | 规则 | alert:rule |
| 工单 | 查看 | ticket:read |
| 工单 | 创建 | ticket:create |
| 工单 | 处理 | ticket:process |
| 工单 | 审批 | ticket:approve |
| AI | 使用 | ai:use |
| AI | 知识库管理 | ai:kb:manage |
| 系统 | 用户管理 | system:user:manage |
| 系统 | 角色管理 | system:role:manage |
| 系统 | 配置管理 | system:config:manage |
| 系统 | 日志审计 | system:audit:read |

#### 11.2.2 数据权限

| 维度 | 说明 |
|------|------|
| **机房权限** | 按机房划分数据可见性 |
| **资产类型** | 限制可查看的设备类型 |
| **资产标签** | 按标签过滤 |
| **告警级别** | 限制可见的告警级别 |

#### 11.2.3 行级权限示例

```sql
-- 用户只能看到自己有权限的机房
CREATE POLICY user_idc_policy ON assets
    FOR SELECT
    USING (
        asset.idc_id IN (
            SELECT idc_id FROM user_idc_permissions
            WHERE user_id = current_setting('app.current_user_id')::UUID
        )
    );
```

### 11.3 角色定义

| 角色 | 描述 | 权限范围 |
|------|------|----------|
| **超级管理员** | 系统最高权限 | 所有权限 |
| **系统管理员** | 系统配置管理 | 用户、角色、系统配置 |
| **运维主管** | 团队管理 | 全部运维权限 + 团队数据 |
| **运维工程师** | 日常运维 | 资产、监控、告警、工单 |
| **只读用户** | 仅查看 | 读权限 |
| **告警值班** | 告警处理 | 告警查看、处理 |
| **访客** | 临时访问 | 极其有限的读权限 |

### 11.4 数据库设计 - 权限

```sql
-- 用户表
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username        VARCHAR(50) NOT NULL UNIQUE,
    email           VARCHAR(255) NOT NULL UNIQUE,
    phone           VARCHAR(20),
    password_hash   VARCHAR(255) NOT NULL,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active',    -- active/inactive/locked
    last_login      TIMESTAMP,
    login_count     INTEGER DEFAULT 0,
    
    -- 多因素认证
    mfa_enabled     BOOLEAN DEFAULT FALSE,
    mfa_secret     VARCHAR(255),
    
    -- 偏好设置
    preferences     JSONB,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID
);

-- 用户组表
CREATE TABLE user_groups (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    description     TEXT,
    parent_id       UUID REFERENCES user_groups(id),
    
    -- 继承设置
    inherit_permissions BOOLEAN DEFAULT TRUE,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 用户-组关联
CREATE TABLE user_group_members (
    user_id         UUID NOT NULL REFERENCES users(id),
    group_id        UUID NOT NULL REFERENCES user_groups(id),
    joined_at       TIMESTAMP DEFAULT NOW(),
    
    PRIMARY KEY (user_id, group_id)
);

-- 角色表
CREATE TABLE roles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(50) NOT NULL,
    description     TEXT,
    is_system       BOOLEAN DEFAULT FALSE,           -- 系统内置角色
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 权限表
CREATE TABLE permissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code            VARCHAR(100) NOT NULL UNIQUE,
    name            VARCHAR(100) NOT NULL,
    module          VARCHAR(50) NOT NULL,
    description     TEXT,
    
    -- 权限类型
    perm_type       VARCHAR(20) NOT NULL,           -- menu/action/data
    data_scope      VARCHAR(50),                    -- self/team/all
    
    -- 层级
    parent_id       UUID REFERENCES permissions(id),
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 角色-权限关联
CREATE TABLE role_permissions (
    role_id         UUID NOT NULL REFERENCES roles(id),
    permission_id   UUID NOT NULL REFERENCES permissions(id),
    
    -- 条件（数据权限）
    conditions      JSONB,
    
    PRIMARY KEY (role_id, permission_id)
);

-- 用户-角色关联
CREATE TABLE user_roles (
    user_id         UUID NOT NULL REFERENCES users(id),
    role_id         UUID NOT NULL REFERENCES roles(id),
    idc_id          UUID,                            -- 数据权限：机房
    expires_at      TIMESTAMP,                      -- 角色过期时间
    
    created_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID,
    
    PRIMARY KEY (user_id, role_id, idc_id)
);

-- 登录会话表
CREATE TABLE user_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    token           VARCHAR(255) NOT NULL,
    ip_address      INET,
    user_agent      TEXT,
    
    -- 会话状态
    status          VARCHAR(20) DEFAULT 'active',   -- active/expired/revoked
    created_at      TIMESTAMP DEFAULT NOW(),
    expires_at      TIMESTAMP NOT NULL,
    last_activity   TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_sessions_token ON user_sessions(token);
CREATE INDEX idx_sessions_user ON user_sessions(user_id);
```

---

## 十二、运维工单管理

### 12.1 工单流程

```
┌─────────────────────────────────────────────────────────────────────┐
│                      工单生命周期                                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌────────┐     ┌────────┐     ┌────────┐     ┌────────┐          │
│   │  创建  │────►│  审核  │────►│  执行  │────►│  验收  │          │
│   └────────┘     └────┬───┘     └────┬───┘     └────┬───┘          │
│                      │              │              │               │
│                      ▼              │              ▼               │
│                ┌──────────┐         │        ┌──────────┐         │
│                │  驳回   │         │        │  完成    │         │
│                └────┬────┘         │        └────┬────┘         │
│                     │              │             │               │
│                     │              ▼             │               │
│                     │        ┌──────────┐        │               │
│                     │        │  进行中  │────────┘               │
│                     │        └────┬────┘                        │
│                     │             │                              │
│                     │             ▼                              │
│                     │       ┌──────────┐                         │
│                     │       │  挂起   │                         │
│                     │       └────┬────┘                         │
│                     │            │                               │
│                     └────────────┘                               │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 12.2 工单类型

| 类型 | 说明 | 审批流程 |
|------|------|----------|
| **变更工单** | 配置变更、软件升级 | 需要审批 |
| **故障工单** | 故障处理、应急响应 | 可直接创建 |
| **维护工单** | 计划性维护、巡检 | 需要审批 |
| **采购工单** | 资产采购、维保续费 | 多级审批 |
| **权限工单** | 账号权限申请 | 需要审批 |
| **咨询工单** | 技术咨询、问题反馈 | 直接处理 |

### 12.3 工单字段

| 字段 | 说明 |
|------|------|
| 工单编号 | 自动生成，如 TKT-20250718-001 |
| 类型 | 变更/故障/维护/采购/权限/咨询 |
| 优先级 | P1(紧急)/P2(高)/P3(中)/P4(低) |
| 标题 | 简要描述 |
| 描述 | 详细说明 |
| 附件 | 相关文档、日志、截图 |
| 关联资产 | 关联的设备/系统 |
| 关联告警 | 触发的告警ID |
| 指派给 | 处理人 |
| 抄送给 | 通知人员 |
| 计划开始/结束 | 计划时间 |
| 实际开始/结束 | 实际时间 |
| 处理记录 | 进度跟踪 |
| 验收结果 | 完成后验收 |

### 12.4 数据库设计 - 工单

```sql
-- 工单表
CREATE TABLE tickets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_no       VARCHAR(50) NOT NULL UNIQUE,
    ticket_type     VARCHAR(50) NOT NULL,          -- change/fault/maintenance/procurement/query
    priority        VARCHAR(20) NOT NULL,          -- P1/P2/P3/P4
    
    -- 基本信息
    title           VARCHAR(255) NOT NULL,
    description     TEXT,
    attachments     JSONB,                         -- 文件列表
    
    -- 关联
    asset_id        UUID REFERENCES assets(id),
    alert_id        UUID REFERENCES alerts(id),
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'created', -- created/approved/running/completed/closed
    current_step    VARCHAR(50),                    -- 当前审批/处理步骤
    
    -- 指派
    creator_id      UUID NOT NULL REFERENCES users(id),
    assignee_id     UUID REFERENCES users(id),
    assignee_group  VARCHAR(50),                    -- 处理组
    
    -- 抄送
    cc_users        UUID[],
    
    -- 时间
    planned_start   TIMESTAMP,
    planned_end     TIMESTAMP,
    actual_start    TIMESTAMP,
    actual_end     TIMESTAMP,
    
    -- 结果
    result          TEXT,
    satisfaction    INTEGER,                        -- 1-5 评分
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 工单流程表
CREATE TABLE ticket_workflows (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id),
    step_name       VARCHAR(100) NOT NULL,
    step_type       VARCHAR(20) NOT NULL,          -- approval/process/notify
    
    -- 处理人
    handler_id      UUID REFERENCES users(id),
    handler_group   VARCHAR(50),
    
    -- 内容
    remark          TEXT,
    attachment      VARCHAR(255),
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'pending', -- pending/approved/rejected/skipped
    started_at      TIMESTAMP,
    completed_at    TIMESTAMP,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 工单审批表
CREATE TABLE ticket_approvals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id),
    approver_id     UUID NOT NULL REFERENCES users(id),
    
    -- 审批信息
    approval_type   VARCHAR(50),                    -- first/second/final
    decision        VARCHAR(20),                   -- approved/rejected
    comment         TEXT,
    
    -- 有效期
    valid_from      TIMESTAMP,
    valid_to        TIMESTAMP,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- SLA配置表
CREATE TABLE ticket_sla (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_type     VARCHAR(50) NOT NULL,
    priority        VARCHAR(20) NOT NULL,
    
    -- 响应时限
    response_time   INTEGER NOT NULL,              -- 分钟
    
    -- 处理时限
    resolve_time    INTEGER NOT NULL,              -- 分钟
    
    -- 升级规则
    escalation_time INTEGER,                       -- 分钟
    escalate_to     UUID REFERENCES users(id),
    
    enabled         BOOLEAN DEFAULT TRUE,
    
    UNIQUE (ticket_type, priority)
);

### 12.5 工单步骤与进度自动计算

#### 12.5.1 步骤进度设计理念

**核心原则**：进度由系统根据实际完成内容**自动计算**，执行人不能直接填写百分比，确保客观性。

```
┌─────────────────────────────────────────────────────────────────────┐
│                      进度计算流程                                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────┐      ┌─────────────┐      ┌─────────────┐      │
│   │  发布工单   │─────►│ AI 建议步骤  │─────►│ 审核员调整  │      │
│   │ (选模板)   │      │ (可修改)     │      │ (确认权重)  │      │
│   └─────────────┘      └─────────────┘      └──────┬──────┘      │
│                                                      │              │
│                                                      ▼              │
│   ┌─────────────┐      ┌─────────────┐      ┌─────────────┐      │
│   │ 系统计算    │◄─────│ 执行人填写   │◄─────│ 开始执行    │      │
│   │ 完成百分比  │      │ 完成数量/步骤│      │             │      │
│   └──────┬──────┘      └─────────────┘      └─────────────┘      │
│          │                                                         │
│          ▼                                                         │
│   ┌─────────────┐      ┌─────────────┐                           │
│   │ 提交审核    │─────►│ 审核员通过   │                           │
│   │            │      │ 或驳回       │                           │
│   └─────────────┘      └─────────────┘                           │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

#### 12.5.2 工单模板库

##### 12.5.2.1 预设工单模板

系统预置常用工单模板，支持关键字搜索：

| 模板名称 | 典型步骤 | 适用场景 |
|----------|----------|----------|
| **安装虚拟机** | 创建VM → 配置资源 → 安装OS → 配置网络 → 验收 | 新业务部署 |
| **克隆虚拟机** | 选择母机 → 克隆 → 修改网络 → 验证 | 批量部署 |
| **服务器上架** | 规划位置 → 物理安装 → 接线 → 通电 → 验收 | 新服务器入场 |
| **服务器配件更换** | 备份配置 → 关机 → 更换配件 → 开机 → 验证 | 硬件维护 |
| **操作系统安全基线配置** | 账户策略 → 密码策略 → 审计策略 → 网络策略 → 验证 | 安全加固 |
| **网络设备配置变更** | 备份配置 → 规划变更 → 审批 → 执行 → 验证 → 记录 | 配置调整 |
| **应用发布** | 打包 → 测试环境验证 → 预发布 → 正式发布 → 监控 | 软件发布 |
| **数据备份** | 确认范围 → 执行备份 → 验证完整性 → 记录 | 数据保护 |
| **故障处理** | 故障定位 → 应急处理 → 根因分析 → 永久修复 → 验收 | 故障响应 |

##### 12.5.2.2 模板选择与搜索

```yaml
# API: POST /api/v1/tickets/templates/search
{
  "keyword": "安全基线",  # 关键字搜索
  "category": "maintenance",  # 可选：按类别过滤
  "limit": 10
}

# 返回匹配模板列表
{
  "templates": [
    {
      "id": "uuid",
      "name": "操作系统安全基线配置",
      "category": "maintenance",
      "steps": [
        {"name": "账户策略配置", "weight": 1, "default_items": "检查本地管理员账户"},
        {"name": "密码策略配置", "weight": 1, "default_items": "密码复杂度要求"},
        {"name": "审计策略配置", "weight": 1, "default_items": "审计日志大小"},
        {"name": "网络策略配置", "weight": 1, "default_items": "禁用不必要的服务"},
        {"name": "最终验证", "weight": 1, "default_items": "全量扫描确认"}
      ]
    },
    ...
  ]
}
```

#### 12.5.3 AI 步骤建议

发布工单时，AI 根据工单标题和描述自动生成步骤建议：

```yaml
# AI 建议示例
工单标题: "为10台服务器配置安全基线"
AI 建议:
  steps:
    - name: "安全基线检查"
      weight: 3
      input_type: "ip_list"  # 需要填写IP列表
      description: "对服务器进行安全基线检查"
      
    - name: "账户策略配置"
      weight: 2
      input_type: "count"  # 可以填数量
      description: "配置账户锁定策略"
      
    - name: "密码策略配置"
      weight: 2
      input_type: "count"
      description: "配置密码复杂度策略"
      
    - name: "审计日志配置"
      weight: 2
      input_type: "count"
      description: "配置审计日志"
      
    - name: "验证确认"
      weight: 1
      input_type: "boolean"  # 完成后确认
      description: "验证所有配置生效"

审核员可以在此基础上：
- 添加/删除/修改步骤
- 调整每个步骤的权重
- 指定输入类型（IP列表/网段/数量/布尔）
```

#### 12.5.4 步骤权重设计

##### 12.5.4.1 权重机制

- **默认权重**：每个步骤默认权重为 1（平均分配）
- **自定义权重**：审核员可以根据步骤的工作量调整权重
- **权重范围**：建议 0.5 ~ 10，支持小数

```yaml
# 权重示例
steps:
  - name: "服务器上架"
    weight: 3   # 体力活，权重高
    estimated_time: "30分钟"
    
  - name: "接线"
    weight: 2
    estimated_time: "20分钟"
    
  - name: "通电测试"
    weight: 1
    estimated_time: "10分钟"
```

##### 12.5.4.2 权重调整权限

| 角色 | 权限 |
|------|------|
| **审核员** | 可在发布工单时设置/调整权重 |
| **执行人** | 只能填写完成情况，不能修改权重 |
| **系统** | 根据权重自动计算百分比 |

#### 12.5.5 执行人填写方式

##### 12.5.5.1 输入类型

执行人根据步骤要求填写完成情况，支持以下类型：

| 输入类型 | 说明 | 示例 |
|----------|------|------|
| **ip_list** | IP地址列表 | 192.168.1.10, 192.168.1.20, 192.168.1.30 |
| **cidr** | 网段/范围 | 192.168.1.0/24 或 192.168.1.10-192.168.1.50 |
| **count** | 数量 | 10（已完成10台） |
| **percentage** | 百分比 | 50（完成50%） |
| **boolean** | 是否完成 | true/false |
| **text** | 文本描述 | 备注说明 |

##### 12.5.5.2 填写示例

```yaml
# 工单步骤
steps:
  - id: "step-1"
    name: "安全基线检查"
    weight: 3
    input_type: "cidr"
    target: "需要检查的服务器网段"
    
  - id: "step-2"
    name: "账户策略配置"
    weight: 2
    input_type: "ip_list"
    target: "需要配置的服务器IP"
    
  - id: "step-3"
    name: "验证确认"
    weight: 1
    input_type: "boolean"
    target: "是否验证通过"

# 执行人填写
progress:
  - step_id: "step-1"
    input_type: "cidr"
    value: "10.0.1.0/24"  # 该网段共20台服务器
    
  - step_id: "step-2"
    input_type: "ip_list"
    value: "10.0.1.10,10.0.1.15,10.0.1.20"  # 已完成3台
    
  - step_id: "step-3"
    input_type: "boolean"
    value: "false"  # 尚未验证
```

#### 12.5.6 进度自动计算算法

##### 12.5.6.1 计算公式

```
总权重 = Σ(各步骤权重)
步骤进度 = (已完成数量 / 目标总数) × 100%
工单进度 = Σ(步骤进度 × 步骤权重) / 总权重
```

##### 12.5.6.2 计算示例

```yaml
# 工单配置
steps:
  - name: "安全基线检查"      weight: 3   target: 20台  # 目标20台
  - name: "账户策略配置"     weight: 2   target: 20台
  - name: "密码策略配置"     weight: 2   target: 20台
  - name: "审计策略配置"     weight: 2   target: 20台
  - name: "验证确认"         weight: 1   target: 1   # 布尔型，完成=1

# 执行人填写
progress:
  - step: "安全基线检查"     completed: 20台  # 20/20 = 100%
  - step: "账户策略配置"     completed: 15台  # 15/20 = 75%
  - step: "密码策略配置"     completed: 15台  # 15/20 = 75%
  - step: "审计策略配置"     completed: 10台  # 10/20 = 50%
  - step: "验证确认"         completed: false # 0/1 = 0%

# 计算过程
总权重 = 3+2+2+2+1 = 10

步骤进度贡献:
  - 安全基线: (20/20) × 100% × 3 = 300
  - 账户策略: (15/20) × 100% × 2 = 150
  - 密码策略: (15/20) × 100% × 2 = 150
  - 审计策略: (10/20) × 100% × 2 = 100
  - 验证确认: (0/1) × 100% × 1 = 0

工单进度 = (300+150+150+100+0) / 10 = 70%
```

##### 12.5.6.3 特殊情况处理

| 场景 | 处理方式 |
|------|----------|
| **网段IP解析** | 系统自动解析网段CIDR，计算实际IP数量 |
| **重复IP** | 自动去重，重复IP只计算一次 |
| **超范围填写** | 如果填写IP不在目标范围内，系统警告 |
| **步骤跳过** | 审核员可标记某步骤不适用，该步骤权重归零 |

#### 12.5.7 审核与驳回机制

##### 12.5.7.1 提交审核

执行人完成填写后提交审核：
- 系统自动计算当前进度百分比
- 附带所有步骤的完成详情
- 提交给审核员

##### 12.5.7.2 审核操作

| 操作 | 说明 |
|------|------|
| **通过** | 确认完成情况，锁定进度 |
| **驳回** | 需要执行人重新填写，填写驳回原因 |
| **要求补充** | 部分信息需要补充说明 |

##### 12.5.7.3 驳回流程

```
执行人提交 ──► 审核员审查 ──► 驳回(说明理由)
    │                              │
    │                              ▼
    │                      执行人重新填写
    │                              │
    │                              ▼
    └─────── 审核通过 ◄────────────┘
    
重新提交后，系统重新计算完成百分比
```

#### 12.5.8 数据库设计 - 工单步骤

```sql
-- 工单模板表
CREATE TABLE ticket_templates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    category        VARCHAR(50),                    -- change/fault/maintenance/procurement/query
    description     TEXT,
    
    -- 预设步骤（JSON定义）
    steps           JSONB NOT NULL,                 -- 步骤定义数组
    
    -- 搜索索引
    keywords        TEXT[],                         -- 搜索关键字
    
    -- 状态
    is_default      BOOLEAN DEFAULT FALSE,
    is_active       BOOLEAN DEFAULT TRUE,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id)
);

-- 工单步骤表（每个工单的步骤实例）
CREATE TABLE ticket_steps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    step_order      INTEGER NOT NULL,               -- 步骤序号
    name            VARCHAR(255) NOT NULL,
    description     TEXT,
    
    -- 权重配置
    weight          DECIMAL(3,1) DEFAULT 1.0,       -- 权重，默认1
    estimated_time  INTEGER,                        -- 预估时间(分钟)
    
    -- 目标配置
    input_type      VARCHAR(20) NOT NULL,           -- ip_list/cidr/count/percentage/boolean/text
    target_value    TEXT,                           -- 目标值，如"20台"或"192.168.1.0/24"
    target_count    INTEGER,                        -- 目标数量（解析后）
    
    -- 完成情况
    completed_value TEXT,                           -- 执行人填写的完成值
    completed_count INTEGER,                        -- 解析后的完成数量
    progress_percent DECIMAL(5,2) DEFAULT 0,        -- 该步骤进度
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'pending',  -- pending/in_progress/completed/skipped
    
    -- 时间
    started_at      TIMESTAMP,
    completed_at    TIMESTAMP,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 步骤进度历史（每次提交记录）
CREATE TABLE step_progress_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    step_id         UUID NOT NULL REFERENCES ticket_steps(id),
    
    -- 提交内容
    submitted_value TEXT,
    submitted_count INTEGER,
    progress_percent DECIMAL(5,2),
    
    -- 审核结果
    audit_status    VARCHAR(20),                    -- pending/approved/rejected
    audit_comment   TEXT,
    audited_by      UUID REFERENCES users(id),
    audited_at      TIMESTAMP,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 工单进度汇总（缓存）
CREATE TABLE ticket_progress (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    
    total_weight    DECIMAL(5,2) NOT NULL,          -- 总权重
    completed_weight DECIMAL(5,2) DEFAULT 0,       -- 已完成权重
    
    progress_percent DECIMAL(5,2) DEFAULT 0,        -- 计算后的进度
    
    last_step_id    UUID,                           -- 最近更新的步骤
    updated_at      TIMESTAMP DEFAULT NOW(),
    
    UNIQUE (ticket_id)
);

-- 索引
CREATE INDEX idx_ticket_steps_ticket ON ticket_steps(ticket_id);
CREATE INDEX idx_step_progress_ticket ON step_progress_history(ticket_id);
CREATE INDEX idx_ticket_progress_ticket ON ticket_progress(ticket_id);
CREATE INDEX idx_ticket_templates_keywords ON ticket_templates USING GIN(keywords);
```

#### 12.5.9 API 设计

##### 12.5.9.1 模板相关

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/tickets/templates | 获取模板列表 |
| GET | /api/v1/tickets/templates/:id | 获取模板详情 |
| POST | /api/v1/tickets/templates/search | 关键字搜索模板 |
| POST | /api/v1/tickets/templates | 创建模板 |
| PUT | /api/v1/tickets/templates/:id | 更新模板 |

##### 12.5.9.2 AI 建议

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/tickets/steps/ai-suggest | AI 生成步骤建议 |

##### 12.5.9.3 工单步骤

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/tickets/:id/steps | 获取工单步骤列表 |
| PUT | /api/v1/tickets/:id/steps | 审核员调整步骤（权重等） |
| PUT | /api/v1/tickets/:id/steps/:stepId/progress | 执行人填写进度 |
| GET | /api/v1/tickets/:id/progress | 获取当前进度 |
| POST | /api/v1/tickets/:id/steps/submit | 提交审核 |

##### 12.5.9.4 审核

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/tickets/:id/steps/audit/approve | 通过审核 |
| POST | /api/v1/tickets/:id/steps/audit/reject | 驳回（需说明理由） |

#### 12.5.10 界面交互设计

##### 12.5.10.1 发布工单页面

```
┌─────────────────────────────────────────────────────────────────┐
│  发布工单                                                         │
├─────────────────────────────────────────────────────────────────┤
│  标题: [________________________________________________]        │
│                                                                 │
│  选择模板: [🔍 搜索模板...                        ▼]            │
│          └─ 操作系统安全基线配置                                  │
│                                                                 │
│  ── AI 建议步骤 (审核员可调整) ──                               │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 1. 安全基线检查         权重:[3] 目标:[20台]             │   │
│  │    输入类型: ○IP列表 ○网段 ○数量 ●布尔                   │   │
│  │ 2. 账户策略配置         权重:[2] 目标:[20台]             │   │
│  │ 3. 密码策略配置         权重:[2] 目标:[20台]             │   │
│  │ 4. 审计策略配置         权重:[2] 目标:[20台]             │   │
│  │ 5. 验证确认             权重:[1] 目标:[1]                │   │
│  └─────────────────────────────────────────────────────────┘   │
│  [+ 添加步骤]  [♻️ AI 重新生成]                                  │
│                                                                 │
│  指派给: [选择执行人...]                                         │
│  审核员: [选择审核人...]                                         │
│                                                                 │
│  [取消]  [保存草稿]  [发布工单]                                   │
└─────────────────────────────────────────────────────────────────┘
```

##### 12.5.10.2 执行人填写页面

```
┌─────────────────────────────────────────────────────────────────┐
│  工单: TKT-20250718-001  安全基线配置     进度: 60%            │
├─────────────────────────────────────────────────────────────────┤
│  步骤进度:                                                       │
│                                                                 │
│  ☑ 安全基线检查           目标: 20台  已完成: 20台   100%       │
│    [10.0.1.10, 10.0.1.15, ...]                                  │
│                                                                 │
│  ☑ 账户策略配置           目标: 20台  已完成: 15台   75%        │
│    填写完成数量/IP列表/网段:                                     │
│    [10.0.1.10-10.0.1.24________________]  (网段)                │
│                                                                 │
│  ⏳ 密码策略配置           目标: 20台  已完成: 10台   50%        │
│    填写完成数量/IP列表/网段:                                     │
│    [10________________________________]  (数量)                  │
│                                                                 │
│  ⏳ 审计策略配置           目标: 20台  已完成: 0台    0%          │
│                                                                 │
│  ⏳ 验证确认               目标: 1     已完成: 否     0%          │
│                                                                 │
│  [保存]  [提交审核]                                              │
└─────────────────────────────────────────────────────────────────┘
```

##### 12.5.10.3 审核员审核页面

```
┌─────────────────────────────────────────────────────────────────┐
│  审核工单: TKT-20250718-001                                     │
├─────────────────────────────────────────────────────────────────┤
│  当前进度: 60%                                                  │
│                                                                 │
│  执行人提交内容:                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 步骤2: 账户策略配置                                      │   │
│  │   填写: 10.0.1.10-10.0.1.19 (10台)                     │   │
│  │   但目标要求20台                                        │   │
│  │   质疑: 为什么只完成了10台？剩余10台什么情况？           │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  [通过]  [驳回(需填写原因)]  [要求补充说明]                       │
└─────────────────────────────────────────────────────────────────┘
```

### 12.6 工单审核与多人协作

#### 12.5.1 审核流程概述

```
┌─────────────────────────────────────────────────────────────────────┐
│                      工单审核与协作流程                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   参与者提交完成 ──► 审核员审核 ──► 通过/驳回                       │
│         │                  │                                        │
│         │                  ▼                                        │
│         │           ┌──────────────┐                              │
│         │           │ 驳回(需说明理由)│                           │
│         │           └──────┬───────┘                              │
│         │                  │                                        │
│         │                  ▼                                        │
│         │           ┌──────────────┐                              │
│         │           │ 返工/补充细节 │                             │
│         │           └──────┬───────┘                              │
│         │                  │                                        │
│         └──────────────────┘                                        │
│                                                                      │
│   分片任务 ──► 各参与者独立完成 ──► 各自审核 ──► 汇总统计            │
│         │         │                                                  │
│         │         ▼                                                  │
│         │   ┌──────────────────────────┐                           │
│         │   │ 独立核算: 速度/数量/通过率 │                           │
│         │   └──────────────────────────┘                           │
│                                                                      │
│   班次交接 ──► 进度交接 ──► 细节补充 ──► 继续执行                    │
│         │                                                            │
│         ▼                                                            │
│   ┌──────────────────────────────────────────────┐                 │
│   │ 交接记录: 已完成/未完成/下一步/待补充/注意事项 │                 │
│   └──────────────────────────────────────────────┘                 │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

#### 12.5.2 审核功能详细设计

##### 12.5.2.1 审核状态

| 状态 | 说明 | 可执行操作 |
|------|------|------------|
| **待审核** | 提交完成，等待审核 | 审核/查看 |
| **审核中** | 审核员正在审核 | 继续审核/撤回 |
| **已通过** | 审核通过 | 查看结果 |
| **已驳回** | 审核不通过，需返工 | 查看原因/返工 |
| **已撤回** | 提交人主动撤回 | 重新提交 |

##### 12.5.2.2 审核操作

```go
// 审核操作类型
type AuditAction string

const (
    AuditApprove  AuditAction = "approve"   // 通过
    AuditReject   AuditAction = "reject"    // 驳回
    AuditRequestMore AuditAction = "request_more"  // 要求补充细节
    AuditWithdraw AuditAction = "withdraw" // 撤回
)

// 审核请求
type AuditRequest struct {
    TicketID     UUID       `json:"ticket_id"`
    SegmentID    UUID       `json:"segment_id"`     // 分片ID（可选）
    AuditAction  AuditAction `json:"action"`
    Comment      string     `json:"comment"`        // 审核意见
    
    // 驳回/补充细节时必填
    RejectReason string     `json:"reject_reason,omitempty"`
    
    // 要求补充细节
    RequiredInfo []string   `json:"required_info,omitempty"`  // 需要补充的信息列表
    DueDate     *time.Time `json:"due_date,omitempty"`      // 补充期限
    
    // 附件
    Attachments  []string   `json:"attachments,omitempty"`   // 审核附件（如截图）
}
```

##### 12.5.2.3 审核配置

```yaml
# ticket-audit-config.yaml
# 工单审核配置

audit:
  # 审核模式
  mode: "manual"  # manual/auto/hybrid
  
  # 审核时限配置
  timeout:
    enabled: true
    hours: 24  # 24小时内必须审核
    on_timeout: "notify"  # notify/warning/escalate/auto_reject
    
  # 驳回配置
  rejection:
    max_attempts: 3  # 最多驳回3次
    require_reason: true  # 必须填写理由
    require_evidence: false  # 必须附上证据
    
  # 通知配置
  notification:
    on_submit: true   # 提交审核时通知
    on_approve: true  # 通过时通知
    on_reject: true   # 驳回时通知
    on_timeout: true  # 超时时通知
```

#### 12.5.3 分片任务功能

##### 12.5.3.1 分片任务概述

```
┌─────────────────────────────────────────────────────────────────────┐
│                     分片任务流程                                      │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   主工单 (安全基线设置 - 100台主机)                                   │
│         │                                                          │
│         ▼                                                          │
│   分片1: 主机1-33 (张三)                                           │
│   分片2: 主机34-66 (李四)                                          │
│   分片3: 主机67-100 (王五)                                          │
│         │                                                          │
│         ▼                                                          │
│   各分片独立: 创建 → 执行 → 提交 → 审核                              │
│         │                                                          │
│         ▼                                                          │
│   汇总统计: 完成度 / 通过率 / 绩效排名                                │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

##### 12.5.3.2 分片任务数据库设计

```sql
-- 工单分片表
CREATE TABLE ticket_segments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    segment_no      INTEGER NOT NULL,
    segment_name    VARCHAR(100) NOT NULL,
    segment_scope   JSONB NOT NULL,
    host_ids        UUID[],
    total_count     INTEGER NOT NULL,
    assignee_id     UUID REFERENCES users(id),
    status          VARCHAR(20) DEFAULT 'pending',
    completed_count INTEGER DEFAULT 0,
    started_at      TIMESTAMP,
    completed_at    TIMESTAMP,
    due_date        TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    UNIQUE (ticket_id, segment_no)
);

-- 分片审核记录表
CREATE TABLE segment_audit_records (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    segment_id      UUID NOT NULL REFERENCES ticket_segments(id) ON DELETE CASCADE,
    auditor_id      UUID NOT NULL REFERENCES users(id),
    audit_action    VARCHAR(20) NOT NULL,
    comment         TEXT,
    reject_reason   TEXT,
    required_info   TEXT[],
    evidence_files  TEXT[],
    audited_at      TIMESTAMP DEFAULT NOW(),
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 分片进度记录表
CREATE TABLE segment_progress (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    segment_id      UUID NOT NULL REFERENCES ticket_segments(id) ON DELETE CASCADE,
    item_type       VARCHAR(50) NOT NULL,
    item_id         UUID NOT NULL,
    status          VARCHAR(20) NOT NULL,
    processed_by    UUID REFERENCES users(id),
    processed_at    TIMESTAMP,
    result          TEXT,
    evidence_files  TEXT[],
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 分片任务统计表
CREATE TABLE segment_statistics (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    segment_id      UUID NOT NULL REFERENCES ticket_segments(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    total_items     INTEGER NOT NULL,
    completed_items INTEGER DEFAULT 0,
    total_time_minutes INTEGER,
    audit_pass_rate DECIMAL(5,2),
    performance_score DECIMAL(5,2),
    calculated_at   TIMESTAMP DEFAULT NOW(),
    UNIQUE (segment_id, user_id)
);
```

#### 12.5.4 人员绩效统计

##### 12.5.4.1 统计指标

| 指标 | 计算方式 | 说明 |
|------|----------|------|
| **完成数量** | SUM(完成分片数) | 累计完成多少任务 |
| **完成率** | 完成数/分配数 × 100% | 分配任务完成比例 |
| **平均处理时间** | 总耗时/完成数 | 每任务平均耗时 |
| **审核通过率** | 通过数/提交数 × 100% | 一次审核通过比例 |
| **质量分数** | 综合计算 (0-100) | 绩效综合评分 |

##### 12.5.4.2 绩效计算公式

```
质量分数 = W1 × 完成率 + W2 × 审核通过率 + W3 × 按时完成率 - W4 × 返工率

其中:
W1 = 0.4 (完成率权重)
W2 = 0.3 (审核通过率权重)
W3 = 0.2 (按时完成率权重)
W4 = 0.1 (返工率扣分)
```

#### 12.5.5 班次交接功能

##### 12.5.5.1 交接流程

```
当前班次人员 ──► 交接准备 ──► 提交交接 ──► 接班确认 ──► 继续执行
                    │
                    ▼
            ┌────────────────┐
            │ 交接内容:      │
            │ 1. 标记已完成  │
            │ 2. 记录当前进度│
            │ 3. 下一步计划 │
            │ 4. 注意事项   │
            │ 5. 待补充细节 │
            └────────────────┘
```

##### 12.5.5.2 交接信息模板

```json
{
  "handoff_no": "HANDOVER-20250718-001",
  "ticket_id": "uuid",
  "handover_from": "张三",
  "handover_to": "李四",
  "handover_time": "2025-07-18T19:45:00Z",
  "overall_progress": "65%",
  "completed_items": ["主机1-30的安全基线检查", "主机1-15的配置变更"],
  "in_progress_items": [{
    "host": "主机16",
    "status": "检查中",
    "completed_steps": ["系统账户检查", "网络安全检查"],
    "pending_steps": ["服务端口检查", "日志审计检查"]
  }],
  "next_steps": [
    {"order": 1, "content": "完成主机16的剩余检查项", "estimated_time": "30分钟", "priority": "高"},
    {"order": 2, "content": "继续主机17-30的配置变更", "estimated_time": "4小时", "priority": "中"}
  ],
  "precautions": [
    {"type": "风险", "content": "主机22的数据库服务需要在业务低峰期重启", "suggestion": "22:00后"},
    {"type": "已知问题", "content": "主机18的SSH服务响应慢", "suggestion": "联系网络团队排查"}
  ],
  "pending_details": [
    {"item": "主机19的异常日志", "status": "已截图待分析", "requirement": "需要接班人查看附件并分析原因"}
  ],
  "confirm_status": "pending",
  "requested_info": []
}
```

##### 12.5.5.3 班次交接数据库设计

```sql
-- 班次交接表
CREATE TABLE ticket_handoffs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    handoff_no      VARCHAR(50) NOT NULL UNIQUE,
    shift_type      VARCHAR(20) NOT NULL,
    shift_date      DATE NOT NULL,
    handover_from  UUID NOT NULL REFERENCES users(id),
    handover_to    UUID NOT NULL REFERENCES users(id),
    handover_time   TIMESTAMP NOT NULL,
    status          VARCHAR(20) DEFAULT 'pending',
    overall_progress DECIMAL(5,2) DEFAULT 0,
    completed_items TEXT[],
    pending_items   TEXT[],
    next_steps      JSONB NOT NULL,
    precautions     JSONB,
    risk_items      JSONB,
    pending_details JSONB,
    attachments     TEXT[],
    handover_remark  TEXT,
    confirm_remark   TEXT,
    handover_confirmed_at TIMESTAMP,
    handover_confirmed_by UUID,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 交接进度明细表
CREATE TABLE handoff_progress (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    handoff_id      UUID NOT NULL REFERENCES ticket_handoffs(id) ON DELETE CASCADE,
    item_type       VARCHAR(50) NOT NULL,
    item_id         UUID,
    item_name       VARCHAR(255) NOT NULL,
    status          VARCHAR(20) NOT NULL,
    progress_percent DECIMAL(5,2) DEFAULT 0,
    processed_by    UUID REFERENCES users(id),
    next_action     VARCHAR(255),
    remark          TEXT,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 交接历史记录表
CREATE TABLE handoff_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id),
    handoff_id      UUID NOT NULL REFERENCES ticket_handoffs(id),
    action          VARCHAR(50) NOT NULL,
    operator_id     UUID NOT NULL REFERENCES users(id),
    content         JSONB NOT NULL,
    operated_at     TIMESTAMP DEFAULT NOW(),
    created_at      TIMESTAMP DEFAULT NOW()
);
```

#### 12.5.6 API设计

##### 12.5.6.1 审核API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/tickets/:id/audit | 提交审核 |
| POST | /api/v1/tickets/:id/audit/approve | 通过审核 |
| POST | /api/v1/tickets/:id/audit/reject | 驳回审核 |
| POST | /api/v1/tickets/:id/audit/request-more | 要求补充细节 |
| GET | /api/v1/tickets/:id/audit/history | 获取审核历史 |

##### 12.5.6.2 分片任务API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/tickets/:id/segments | 创建分片 |
| GET | /api/v1/tickets/:id/segments | 获取分片列表 |
| PUT | /api/v1/tickets/segments/:id/assign | 指派分片 |
| PUT | /api/v1/tickets/segments/:id/complete | 提交分片完成 |
| GET | /api/v1/users/:id/performance | 获取用户绩效 |

##### 12.5.6.3 班次交接API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/tickets/:id/handoff | 创建交接 |
| GET | /api/v1/tickets/handoffs/:id | 获取交接详情 |
| PUT | /api/v1/tickets/handoffs/:id/confirm | 接班确认 |
| PUT | /api/v1/tickets/handoffs/:id/request-info | 要求补充细节 |
| GET | /api/v1/tickets/handoffs/history | 获取交接历史 |

```

---

## 十三、日志审计与Splunk集成

### 13.1 审计日志类型

| 类型 | 内容 | 保留期限 |
|------|------|----------|
| **操作审计** | 用户登录、权限变更、配置修改 | 1年 |
| **数据审计** | 数据增删改、导出操作 | 1年 |
| **告警审计** | 告警触发、确认、处理 | 6个月 |
| **工单审计** | 工单创建、审批、完成 | 1年 |
| **系统审计** | 服务启动停止、定时任务 | 1年 |
| **API审计** | API调用日志、异常 | 6个月 |
| **安全审计** | 认证失败、异常访问 | 1年 |

### 13.2 审计日志字段

```json
{
  "timestamp": "2025-07-18T10:30:00Z",
  "event_type": "asset.update",
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "username": "admin",
  "ip_address": "192.168.1.100",
  "user_agent": "Mozilla/5.0...",
  
  "resource_type": "asset",
  "resource_id": "660e8400-e29b-41d4-a716-446655440001",
  "action": "update",
  
  "before": {
    "status": "active",
    "rack_position": "12U"
  },
  "after": {
    "status": "active",
    "rack_position": "15U"
  },
  
  "changes": [
    {
      "field": "rack_position",
      "old_value": "12U",
      "new_value": "15U"
    }
  ],
  
  "result": "success",
  "error_message": null,
  "request_id": "req-abc123",
  "correlation_id": "corr-xyz789"
}
```

### 13.3 Splunk集成架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                     日志审计架构                                      │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────────────────────────────────────────────────┐  │
│   │                       应用服务                               │  │
│   │   (资产服务/监控服务/告警服务/工单服务...)                   │  │
│   └────────────────────────────┬────────────────────────────────┘  │
│                                │                                     │
│                                ▼                                     │
│   ┌─────────────────────────────────────────────────────────────┐  │
│   │                    审计日志收集                              │  │
│   │   ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │  │
│   │   │  文件输出 │  │  Syslog  │  │  Fluentd │  │  API推送 │  │  │
│   │   └──────────┘  └──────────┘  └──────────┘  └──────────┘  │  │
│   └────────────────────────────┬────────────────────────────────┘  │
│                                │                                     │
│                                ▼                                     │
│   ┌─────────────────────────────────────────────────────────────┐  │
│   │                    日志处理层                                │  │
│   │   ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │  │
│   │   │  格式化   │  │  富化    │  │  过滤    │  │  缓冲    │  │  │
│   │   └──────────┘  └──────────┘  └──────────┘  └──────────┘  │  │
│   └────────────────────────────┬────────────────────────────────┘  │
│                                │                                     │
│              ┌─────────────────┼─────────────────┐                  │
│              │                 │                 │                  │
│              ▼                 ▼                 ▼                  │
│   ┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐   │
│   │   本地日志库     │ │    Splunk HF    │ │   Kafka/Redis   │   │
│   │   (Elasticsearch)│ │   (Forwarder)  │ │   (消息队列)    │   │
│   └──────────────────┘ └──────────────────┘ └──────────────────┘   │
│              │                 │                 │                  │
│              │                 │                 │                  │
│              └─────────────────┼─────────────────┘                  │
│                                │                                     │
│                                ▼                                     │
│   ┌─────────────────────────────────────────────────────────────┐  │
│   │                    Splunk Enterprise                       │  │
│   │   ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐│  │
│   │   │  索引    │  │  搜索    │  │  可视化  │  │  告警    ││  │
│   │   └──────────┘  └──────────┘  └──────────┘  └──────────┘│  │
│   └─────────────────────────────────────────────────────────────┘  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 13.4 日志转发配置

#### 13.4.1 Syslog转发

```yaml
# rsyslog配置
template(
    name="SplunkFormat"
    type="string"
    string="%msg:R,ERE,1,ZERO:([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z)--end%\n"
)

*.* action(
    type="omfwd"
    target="splunk.company.com"
    port="514"
    protocol="tcp"
    template="SplunkFormat"
    ResendLastMSGOnFailure="on"
    QueueFileName="splunk_forward"
    QueueMaxDiskSpace="1g"
    QueueSize="100000"
)
```

#### 13.4.2 HTTP Event Collector (HEC)

```go
// 日志发送到Splunk HEC
type SplunkHEC struct {
    endpoint string
    token    string
    index    string
}

func (s *SplunkHEC) Send(event AuditEvent) error {
    payload := map[string]interface{}{
        "event": event,
        "sourcetype": "audit_log",
        "index": s.index,
        "time": event.Timestamp,
    }

    body, _ := json.Marshal(payload)
    
    req, _ := http.NewRequest("POST", s.endpoint+"/services/collector/event", bytes.NewReader(body))
    req.Header.Set("Authorization", "Splunk "+s.token)
    req.Header.Set("Content-Type", "application/json")
    
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        return fmt.Errorf("splunk hec error: %s", resp.Status)
    }
    return nil
}
```

#### 13.4.3 Splunk查询示例

```spl
# 查询所有失败登录
index=audit_logs sourcetype="audit_log" event_type="auth.login" result="failed"

# 查询资产变更记录
index=audit_logs event_type="asset.update" 
| stats count by user_id, changes.field, changes.new_value
| sort -count

# 查询高权限操作
index=audit_logs action IN ("user.create", "role.assign", "config.update")
| table _time, user_id, username, action, ip_address
```

#### 13.4.4 安全设备集成 (IPS/WAF)

> 用户已有 IPS、WAF 等安全设备，其日志已接入 Splunk。可以通过 Splunk 规则触发告警动作，与本平台联动。

##### 13.4.4.1 集成架构

```
┌─────────────────────────────────────────────────────────────────┐
│                  安全设备集成架构                                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  安全设备                    Splunk                              │
│  ┌─────────┐              ┌──────────────┐                       │
│  │ IPS    │─────────────▶│  日志索引    │                       │
│  └─────────┘              └──────┬───────┘                       │
│  ┌─────────┐                     │                                │
│  │ WAF    │─────────────────────┤                                │
│  └─────────┘                     │                                │
│  ┌─────────┐                     │       本平台                   │
│  │ 防火墙  │─────────────────────┼────▶ ┌──────────────┐        │
│  └─────────┘                     │       │  告警接收    │        │
│                                  │       │  (Webhook)  │        │
│                                  │       └──────┬───────┘        │
│                                  │              │                 │
│                                  │              ▼                 │
│                                  │       ┌──────────────┐        │
│                                  └──────▶│  告警管理    │        │
│                                          └──────────────┘        │
│                                          - 告警处理              │
│                                          - 工单创建              │
│                                          - 通知推送              │
└─────────────────────────────────────────────────────────────────┘
```

##### 13.4.4.2 支持的安全设备

| 设备类型 | 日志格式 | 监控内容 |
|----------|----------|----------|
| **IPS/IDS** | 攻击告警、阻断日志、威胁事件 | 攻击类型、源IP、目的IP、威胁等级 |
| **WAF** | 攻击拦截、CC攻击、SQL注入、XSS | 攻击特征、URL、Cookie、请求头 |
| **防火墙** | 访问控制日志、NAT日志、连接日志 | 允许/拒绝、源目的IP、端口 |
| **堡垒机** | 操作审计、会话录像、命令记录 | 操作人、目标主机、执行命令 |
| **数据库审计** | SQL审计、敏感操作 | 用户、SQL语句、查询结果 |

##### 13.4.4.3 Splunk 告警动作配置

在 Splunk 中配置告警动作，调用本平台 Webhook：

```yaml
# Splunk 告警动作配置 (alert_actions.conf)
[netmonitor_webhook]
param.api_url = https://monitor.example.com/api/v1/alerts/external
param.method = POST
param.token = $SECRET_TOKEN$
param.content_type = json
param.payload = {
    "alert_name": "$name$",
    "severity": "$result.severity$",
    "source_ip": "$result.src_ip$",
    "dest_ip": "$result.dst_ip$",
    "attack_type": "$result.attack_type$",
    "raw_log": "$result.raw$",
    "splunk_search": "$search$",
    "splunk_link": "$results.link$"
}
```

##### 13.4.4.4 Splunk 告警规则示例

**IPS 威胁告警**：

```
index=ips_logs severity=critical
| stats count by src_ip, dest_ip, attack_name
| where count > 5
| alert name="IPS威胁事件"
| severity="critical"
```

**WAF 攻击拦截**：

```
index=waf_logs action=block
| stats count by client_ip, uri, attack_type
| where count > 10
| alert name="WAF攻击告警"
| severity="high"
```

**暴力破解检测**：

```
index=firewall_logs action=deny 
| stats dc(user) as unique_users by dest_ip
| where unique_users > 10
| alert name="暴力破解检测"
| severity="medium"
```

##### 13.4.4.5 告警接收接口

```go
// 外部告警接收 API
type ExternalAlertRequest struct {
    // 告警信息
    AlertName   string `json:"alert_name"`
    Severity    string `json:"severity"`   // critical/high/medium/low
    Message     string `json:"message"`
    
    // 事件详情
    SourceIP    string `json:"source_ip"`
    DestIP      string `json:"dest_ip"`
    Port        int    `json:"port"`
    Protocol    string `json:"protocol"`
    
    // 攻击信息
    AttackType  string `json:"attack_type"`
    AttackName  string `json:"attack_name"`
    RawLog      string `json:"raw_log"`
    
    // 来源
    Source      string `json:"source"`       // ips/waf/firewall
    Vendor      string `json:"vendor"`       // 厂商
    SplunkSearch string `json:"splunk_search"`
    SplunkLink  string `json:"splunk_link"`
    
    // 时间
    EventTime   time.Time `json:"event_time"`
}

@router.Post("/api/v1/alerts/external")
func ReceiveExternalAlert(c *gin.Context) {
    var req ExternalAlertRequest
    c.ShouldBindJSON(&req)
    
    // 转换为本平台告警
    alert := Alert{
        Title:      req.AlertName,
        Severity:   convertSeverity(req.Severity),
        Source:     "splunk:" + req.Source,
        Description: req.Message,
        AssetIP:    req.DestIP,
        Tags: map[string]string{
            "source_ip":   req.SourceIP,
            "dest_ip":    req.DestIP,
            "attack_type": req.AttackType,
            "vendor":     req.Vendor,
        },
    }
    
    // 保存并发送通知
    alertService.Create(alert)
}
```

##### 13.4.4.6 安全设备接入配置表

```sql
-- 外部告警源配置
CREATE TABLE alert_sources (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    
    -- 来源类型
    source_type     VARCHAR(20) NOT NULL,    -- splunk/custom
    vendor          VARCHAR(50),              -- 厂商
    
    -- 接入配置
    webhook_url     VARCHAR(500),
    api_key         VARCHAR(255),             -- 加密存储
    secret_token    VARCHAR(255),             -- 签名密钥
    
    -- 解析配置
    field_mapping   JSONB,                   -- 字段映射
    severity_map    JSONB,                   -- 级别映射
    
    -- 状态
    is_enabled      BOOLEAN DEFAULT TRUE,
    last_event_at   TIMESTAMP,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP
);

-- 初始化数据
INSERT INTO alert_sources (name, source_type, vendor, webhook_url) VALUES
('Splunk-IPS告警', 'splunk', 'IPS', 'https://monitor.example.com/api/v1/alerts/external'),
('Splunk-WAF告警', 'splunk', 'WAF', 'https://monitor.example.com/api/v1/alerts/external'),
('Splunk-防火墙告警', 'splunk', 'Firewall', 'https://monitor.example.com/api/v1/alerts/external');
```

##### 13.4.4.7 安全日报/周报导入

支持导入 IPS/WAF 等安全设备的定期报表：

```sql
-- 安全报告表
CREATE TABLE security_reports (
    id              UUID PRIMARY KEY,
    report_type     VARCHAR(20) NOT NULL,    -- daily/weekly/monthly
    device_type     VARCHAR(20) NOT NULL,    -- ips/waf/firewall
    device_name     VARCHAR(100),
    
    -- 报告时间
    report_period   DATE NOT NULL,
    
    -- 统计信息
    total_events    INTEGER,                  -- 总事件数
    critical_events INTEGER,                  -- 严重事件
    blocked_events  INTEGER,                  -- 拦截事件
    allowed_events  INTEGER,                  -- 放行事件
    
    -- TOP统计
    top_attacks     JSONB,                   -- TOP攻击类型
    top_sources     JSONB,                   -- TOP攻击源
    top_targets     JSONB,                   -- TOP攻击目标
    
    -- 原始报告
    file_path       VARCHAR(500),
    file_hash       VARCHAR(64),
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'pending',
    
    uploaded_by     UUID REFERENCES users(id),
    uploaded_at     TIMESTAMP DEFAULT NOW()
);

-- 报表上传界面
-- 支持 PDF/Excel/CSV 格式
-- 自动解析关键指标
```

##### 13.4.4.8 统一告警视图

安全设备告警与本平台告警统一展示：

```
告警列表：

┌─────────────────────────────────────────────────────────────────┐
│  来源: [全部 ▼]  级别: [全部 ▼]   时间: [今天 ▼]             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  🔴 WAF-攻击拦截  POST /login SQL注入    192.168.1.100  10:30 │
│     设备: WAF-01  攻击特征: 'OR 1=1'                            │
│                                                                  │
│  🔴 IPS-威胁事件  恶意软件   10.0.0.55   10:25                │
│     设备: IPS-01  威胁类型: Trojan.Downloader                  │
│                                                                  │
│  🟡 网络-端口扫描  10.0.0.88   10:15                           │
│     设备: Firewall-01  扫描端口: 1-1024                        │
│                                                                  │
│  🟢 系统-CPU告警   192.168.1.10  09:55                         │
│     平台: 监控系统  当前: 95%  阈值: 80%                        │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

#### 13.4.5 文件完整性监控 (Tripwire/AIDE)

> **现状**：Linux 系统安装了 Tripwire 监控本地文件变化，每日发邮件汇报，人工分析难度大。
> **目标**：报告自动入库 + AI 智能分析。

##### 13.4.5.1 集成方案

```
┌─────────────────────────────────────────────────────────────────┐
│               文件完整性监控集成架构                                │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   主机 Tripwire                    本平台                        │
│   ┌─────────────┐                 ┌──────────────┐              │
│   │ 定时检测   │───────▶│   邮件/Syslog  │─────▶ 报告入库    │
│   │ (每日/定时)│         │   (原始报告)   │        │           │
│   └─────────────┘         └──────────────┘        │           │
│                                                        │           │
│                                                        ▼           │
│                                               ┌──────────────┐   │
│                                               │  AI 分析引擎  │   │
│                                               │              │   │
│                                               │  - 变更分类   │   │
│                                               │  - 风险评估   │   │
│                                               │  - 建议操作   │   │
│                                               └──────┬───────┘   │
│                                                      │            │
│                                                      ▼            │
│                                               ┌──────────────┐   │
│                                               │  告警/工单   │   │
│                                               │              │   │
│                                               │  - 风险告警  │   │
│                                               │  - 整改工单  │   │
│                                               └──────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

##### 13.4.5.2 报告接入方式

| 方式 | 说明 | 配置 |
|------|------|------|
| **邮件接收** | Tripwire 报告发送到指定邮箱 | 邮箱规则 → IMAP 拉取 |
| **Syslog** | 发送到 syslog 服务器 | rsyslog 转发 |
| **API 推送** | 定时调用 API 上报 | 脚本调用 |

```bash
# Tripwire 报告推送脚本示例
#!/bin/bash
# 每日定时执行，将报告发送到本平台

TRIPWIRE_REPORT="/var/lib/tripwire/report/$(hostname)-$(date +%Y%m%d).twr"
API_URL="https://monitor.example.com/api/v1/tripwire/reports"

curl -X POST $API_URL \
    -H "Authorization: Bearer $API_KEY" \
    -F "report_file=@$TRIPWIRE_REPORT" \
    -F "hostname=$(hostname)" \
    -F "report_date=$(date +%Y-%m-%d)"
```

##### 13.4.5.3 报告解析与入库

```sql
-- Tripwire 报告表
CREATE TABLE file_integrity_reports (
    id              UUID PRIMARY KEY,
    hostname        VARCHAR(100) NOT NULL,
    report_date     DATE NOT NULL,
    
    -- 原始报告
    raw_report      TEXT,
    file_hash       VARCHAR(64),
    
    -- 统计信息
    total_changes   INTEGER DEFAULT 0,
    added_files     INTEGER DEFAULT 0,
    removed_files   INTEGER DEFAULT 0,
    modified_files  INTEGER DEFAULT 0,
    
    -- 严重程度分类
    critical_count  INTEGER DEFAULT 0,   # 关键文件变更
    warning_count  INTEGER DEFAULT 0,    # 警告文件变更
    info_count     INTEGER DEFAULT 0,    # 一般变更
    
    -- AI 分析结果
    ai_analysis     JSONB,               # AI 分析结果
    risk_level      VARCHAR(20),         -- critical/high/medium/low/none
    ai_summary      TEXT,                # AI 摘要
    recommendations JSONB,               # AI 建议操作
    
    -- 关联
    asset_id        UUID REFERENCES assets(id),
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'pending',  -- pending/analyzed/acknowledged
    
    imported_at     TIMESTAMP DEFAULT NOW(),
    analyzed_at     TIMESTAMP
);

CREATE INDEX idx_tripwire_host_date ON file_integrity_reports(hostname, report_date);

-- 文件变更明细表
CREATE TABLE file_changes (
    id              UUID PRIMARY KEY,
    report_id       UUID REFERENCES file_integrity_reports(id) ON DELETE CASCADE,
    
    -- 文件信息
    file_path       VARCHAR(500) NOT NULL,
    file_type       VARCHAR(20),         -- regular/directory/symlink
    file_mode       VARCHAR(10),
    file_owner      VARCHAR(50),
    file_group      VARCHAR(50),
    file_size       BIGINT,
    
    -- 变更类型
    change_type     VARCHAR(20) NOT NULL,  -- added/removed/modified
    
    -- 变更前后对比
    old_md5         VARCHAR(32),
    new_md5         VARCHAR(32),
    old_sha256      VARCHAR(64),
    new_sha256      VARCHAR(64),
    
    -- 分类
    category        VARCHAR(50),          -- system/config/log/temp/userdata
    
    -- 风险等级
    risk_level      VARCHAR(20),         -- critical/high/medium/low
    risk_reason     TEXT,                -- 风险说明
    
    -- AI 分析
    ai_analysis     JSONB,               # AI 详细分析
    
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_file_changes_report ON file_changes(report_id);
CREATE INDEX idx_file_changes_path ON file_changes(file_path);
CREATE INDEX idx_file_changes_risk ON file_changes(risk_level);
```

##### 13.4.5.4 AI 分析引擎

AI 自动分析 Tripwire 报告，核心功能：

| 功能 | 说明 |
|------|------|
| **变更分类** | 系统文件/配置文件/日志/临时文件/用户数据 |
| **风险评估** | 评估变更的风险等级 |
| **原因分析** | 分析变更可能的原因 |
| **建议操作** | 给出修复/处理建议 |
| **关联分析** | 与历史变更对比，识别异常模式 |

```python
# AI 分析示例
{
    "report_summary": "检测到 15 个文件变更",
    "critical_changes": [
        {
            "file": "/etc/passwd",
            "change": "modified",
            "risk": "critical",
            "reason": "系统用户文件被修改，可能存在提权风险",
            "ai_analysis": "检测到新增用户 'hacker'，UID=0，建议立即排查"
        },
        {
            "file": "/etc/cron.d/backdoor",
            "change": "added",
            "risk": "critical", 
            "reason": "发现可疑定时任务，可能为后门程序",
            "ai_analysis": "定时执行未知脚本，建议立即删除并检查日志"
        }
    ],
    "recommendations": [
        {
            "priority": 1,
            "action": "检查 /etc/passwd 新增用户",
            "severity": "critical"
        },
        {
            "priority": 2,
            "action": "删除可疑定时任务 /etc/cron.d/backdoor",
            "severity": "critical"
        },
        {
            "priority": 3,
            "action": "审查 /var/log 目录下的日志文件变更",
            "severity": "warning"
        }
    ]
}
```

##### 13.4.5.5 AI 分析提示词

```markdown
你是一个文件完整性监控分析专家。请分析以下 Tripwire 报告：

## 主机信息
- 主机名: {hostname}
- 报告日期: {report_date}
- 变更文件数: {total_changes}

## 变更文件列表
{changes_list}

## 分析要求

1. **风险分类**：将每个变更分类为 critical/high/medium/low
   - critical: 系统关键文件(/etc/passwd, /etc/shadow, /etc/cron.d 等)
   - high: 配置文件(/etc/*.conf, /etc/*.cfg 等)
   - medium: 日志文件
   - low: 普通文件

2. **原因分析**：分析每个变更的可能原因
   - 正常变更（系统更新、配置修改）
   - 可疑变更（未知修改、疑似攻击）
   - 紧急变更（后门、恶意软件）

3. **建议操作**：针对高风险变更给出处理建议

4. **总结**：用一句话总结该报告的安全状况
```

##### 13.4.5.6 告警与工单联动

```
AI 分析完成
     │
     ▼
检查风险等级
     │
   ┌─┴─┐
Critical  其他
   │   │
   ▼   ▼
生成告警  记录归档
   │   │
   ▼   ▼
创建工单  (可选)
   │
   ▼
通知安全管理员
```

**告警规则**：
- critical 变更 → 立即告警 + 紧急工单
- high 变更 → 告警 + 普通工单
- medium 变更 → 记录 + 周报汇总

##### 13.4.5.7 管理界面

```
文件完整性监控：

┌─────────────────────────────────────────────────────────────────┐
│  主机: [全部 ▼]  风险: [全部 ▼]  日期: [最近7天 ▼]            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  主机             日期        变更数   风险   状态    操作       │
│  ─────────────────────────────────────────────────────────────   │
│  web-server-01   2024-02-14   15      🔴高   已分析   [查看]   │
│  db-server-01    2024-02-14   3       🟡中   已分析   [查看]   │
│  app-server-02   2024-02-13   8       🟢低   已分析   [查看]   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘

变更详情：

┌─────────────────────────────────────────────────────────────────┐
│  文件路径                    类型     风险    AI分析             │
│  ─────────────────────────────────────────────────────────────  │
│  /etc/passwd               修改     🔴高    新增用户 hacker    │
│  /etc/cron.d/backdoor      新增     🔴高    可疑定时任务       │
│  /var/log/messages         修改     🟡中    日志轮转           │
│  /tmp/suspicious.sh        新增     🔴高    可疑脚本          │
└─────────────────────────────────────────────────────────────────┘
```

##### 13.4.5.8 关键文件监控规则

内置关键文件列表，自动识别高风险变更：

```sql
-- 关键文件规则表
CREATE TABLE critical_file_rules (
    id              UUID PRIMARY KEY,
    file_pattern    VARCHAR(200) NOT NULL,
    
    -- 匹配规则
    is_regex        BOOLEAN DEFAULT FALSE,
    
    -- 风险等级
    default_risk    VARCHAR(20) DEFAULT 'high',
    
    -- 分类
    category        VARCHAR(50) NOT NULL,  -- system/config/cron/敏感
    
    -- 是否监控
    is_monitored    BOOLEAN DEFAULT TRUE,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 初始化关键文件规则
INSERT INTO critical_file_rules (file_pattern, category, default_risk) VALUES
-- 系统用户
('/etc/passwd', 'system', 'critical'),
('/etc/shadow', 'system', 'critical'),
('/etc/group', 'system', 'critical'),
('/etc/sudoers', 'system', 'critical'),
-- 定时任务
('/etc/cron', 'cron', 'critical'),
('/var/spool/cron', 'cron', 'critical'),
('/etc/cron.d', 'cron', 'critical'),
-- 网络配置
('/etc/hosts', 'config', 'high'),
('/etc/resolv.conf', 'config', 'high'),
('/etc/network', 'config', 'high'),
-- 认证
('/etc/ssh/sshd_config', 'config', 'high'),
('/root/.ssh', 'sensitive', 'critical'),
-- 可执行
('/usr/bin', 'binary', 'medium'),
('/usr/sbin', 'binary', 'medium'),
('/bin', 'binary', 'medium'),
('/sbin', 'binary', 'medium');
```

### 13.5 数据库设计 - 审计

```sql
-- 审计日志表 (本地存储)
CREATE TABLE audit_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 事件信息
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    event_type      VARCHAR(100) NOT NULL,
    
    -- 用户信息
    user_id         UUID REFERENCES users(id),
    username        VARCHAR(50),
    ip_address      INET,
    user_agent      TEXT,
    
    -- 资源信息
    resource_type   VARCHAR(50),
    resource_id     VARCHAR(100),
    action          VARCHAR(50),
    
    -- 变更数据
    before_data     JSONB,
    after_data      JSONB,
    changes         JSONB,
    
    -- 结果
    result          VARCHAR(20),                   -- success/failed
    error_message   TEXT,
    
    -- 追踪
    request_id      VARCHAR(100),
    correlation_id  VARCHAR(100),
    
    -- 索引字段
    event_date      DATE GENERATED ALWAYS AS (timestamp::DATE) STORED
);

-- 索引
CREATE INDEX idx_audit_timestamp ON audit_logs(timestamp DESC);
CREATE INDEX idx_audit_event_type ON audit_logs(event_type);
CREATE INDEX idx_audit_user_id ON audit_logs(user_id);
CREATE INDEX idx_audit_resource ON audit_logs(resource_type, resource_id);

-- 分区表 (按月分区)
CREATE TABLE audit_logs_partitioned (
    LIKE audit_logs INCLUDING ALL
) PARTITION BY RANGE (timestamp);

-- 创建月度分区
CREATE TABLE audit_logs_2025_07 PARTITION OF audit_logs_partitioned
    FOR VALUES FROM ('2025-07-01') TO ('2025-08-01');

CREATE TABLE audit_logs_2025_08 PARTITION OF audit_logs_partitioned
    FOR VALUES FROM ('2025-08-01') TO ('2025-09-01');
```

---

## 十四、模块松耦合设计

### 14.1 设计原则

```
┌─────────────────────────────────────────────────────────────────────┐
│                    松耦合架构原则                                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  1. 独立部署    各模块可独立部署，不相互依赖                          │
│  2. 消息解耦    模块间通过消息队列通信                                │
│  3. 容错隔离    单模块故障不影响其他模块                              │
│  4. 优雅降级    故障模块可自动降级处理                                │
│  5. 自我恢复   故障恢复后自动同步数据                                │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 14.2 架构图

```
┌─────────────────────────────────────────────────────────────────────┐
│                     微服务松耦合架构                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────┐                                                   │
│   │  API网关    │                                                   │
│   │  (Nginx)   │                                                   │
│   └──────┬──────┘                                                   │
│          │                                                          │
│   ┌──────┴──────┐     ┌─────────────────────────────┐              │
│   │             │     │       消息总线 (NATS)       │              │
│   │  ┌───────┐ │     │                             │              │
│   │  │用户服务│ │     │  ┌────────┐ ┌────────┐    │              │
│   │  └─────┬─┘ │     │  │资产事件│ │告警事件│    │              │
│   │        │   │     │  └────┬───┘ └───┬───┘    │              │
│   │  ┌─────▼─┐ │     │       │         │        │              │
│   │  │资产服务│◄┼─────┼───────┴─────────┴────┐   │              │
│   │  └─────┬─┘ │     │                     │    │              │
│   │        │   │     │  ┌────────┐ ┌────────┐│   │              │
│   │  ┌─────▼─┐ │     │  │监控事件│ │工单事件││   │              │
│   │  │监控服务│ │     │  └────┬───┘ └───┬───┘│   │              │
│   │  └─────┬─┘ │     │       │         │    │   │              │
│   │        │   │     │       └─────────┴────┘   │              │
│   │  ┌─────▼─┐ │     │                         │              │
│   │  │告警服务│ │     └─────────────────────────┘              │
│   │  └─────┬─┘ │                                                   │
│   │        │   │                                                   │
│   │  ┌─────▼─┐ │                                                   │
│   │  │AI服务  │ │                                                   │
│   │  └───────┘ │                                                   │
│   │             │                                                   │
│   └──────┬──────┘                                                   │
│          │                                                          │
│   ┌──────┴───────────────────────────────────────────────────┐      │
│   │                      数据层                              │      │
│   │   ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐       │      │
│   │   │PostgreSQL│ │Timescale│ │  Redis │ │MongoDB│       │      │
│   │   └────────┘  └────────┘  └────────┘  └────────┘       │      │
│   └─────────────────────────────────────────────────────────┘      │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 14.3 容错机制

#### 14.3.1 消息队列隔离

```go
// NATS JetStream主题隔离
const (
    SubjectAssetEvents    = "events.assets.>"
    SubjectAlertEvents   = "events.alerts.>"
    SubjectMonitorEvents = "events.monitor.>"
    SubjectTicketEvents  = "events.tickets.>"
    SubjectAIEvents      = "events.ai.>"
)

// 订阅者分组 (相同分组共享负载，不同分组独立消费)
const (
    ConsumerGroupAsset  = "asset-processor"
    ConsumerGroupAlert  = "alert-processor"
    ConsumerGroupAI     = "ai-processor"
)
```

#### 14.3.2 服务降级策略

```go
// 服务状态枚举
type ServiceStatus string

const (
    StatusHealthy   ServiceStatus = "healthy"   // 正常运行
    StatusDegraded ServiceStatus = "degraded"  // 降级运行
    StatusDown     ServiceStatus = "down"      // 服务不可用
)

// 降级策略
var DegradationRules = []DegradationRule{
    {
        Service:   "ai-service",
        DependsOn: "vector-db",
        OnDown:    "disable-rag",              // 禁用RAG功能
    },
    {
        Service:   "alert-service",
        DependsOn: "notification-channels",
        OnDown:    "queue-notifications",       // 通知入队延迟发送
    },
    {
        Service:   "monitor-service",
        DependsOn: "timescaledb",
        OnDown:    "cache-last-values",          // 使用缓存数据
    },
}
```

#### 14.3.3 断路器设计

```go
// 断路器配置
type CircuitBreaker struct {
    name             string
    failureThreshold int           // 失败次数阈值
    recoveryTimeout  time.Duration // 恢复超时
    
    // 状态
    state            State
    failureCount     int
    lastFailure      time.Time
}

// 熔断器状态
type State int

const (
    StateClosed State = iota  // 正常
    StateOpen                 // 熔断
    StateHalfOpen             // 半开尝试
)
```

### 14.4 故障隔离示例

| 故障模块 | 影响范围 | 降级方案 |
|----------|----------|----------|
| **AI服务宕机** | 智能分析、问答 | 显示"AI服务暂时不可用"，手动操作 |
| **拓扑服务宕机** | 拓扑图显示 | 显示静态拓扑图，标注"实时数据暂停" |
| **告警服务宕机** | 告警触发 | 数据本地缓存，恢复后补发 |
| **通知服务宕机** | 通知发送 | 通知入队，批量重试 |
| **数据库主节点** | 写入失败 | 切换到从节点，或暂停非关键写入 |
| **Redis缓存** | 缓存失效 | 直接查数据库，性能下降 |

---

## 十五、安全合规设计

### 15.1 Web安全要求

#### 15.1.1 TLS配置标准

```nginx
# Nginx TLS配置 - PCI-DSS合规
server {
    listen 443 ssl http2;
    server_name monitor.example.com;

    # 证书配置
    ssl_certificate /etc/ssl/certs/monitor.crt;
    ssl_certificate_key /etc/ssl/private/monitor.key;
    
    # TLS协议版本 (禁用SSL和TLS 1.0/1.1)
    ssl_protocols TLSv1.2 TLSv1.3;
    
    # 加密套件 (推荐顺序)
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers on;
    
    # 额外安全头
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline';" always;
    
    # Session安全
    ssl_session_timeout 1d;
    ssl_session_cache shared:SSL:50m;
    ssl_session_tickets off;
    
    # OCSP Stapling
    ssl_stapling on;
    ssl_stapling_verify on;
    resolver 8.8.8.8 8.8.4.4 valid=300s;
    resolver_timeout 5s;
}
```

#### 15.1.2 禁用低版本TLS

| 协议版本 | 状态 | 说明 |
|----------|------|------|
| SSL 2.0 | ❌ 禁用 | 不安全 |
| SSL 3.0 | ❌ 禁用 | POODLE攻击 |
| TLS 1.0 | ❌ 禁用 | BEAST攻击 |
| TLS 1.1 | ❌ 禁用 | 已知漏洞 |
| TLS 1.2 | ✅ 启用 | 最低要求 |
| TLS 1.3 | ✅ 启用 | 推荐 |

### 15.2 密码策略

| 要求 | 配置 |
|------|------|
| 最小长度 | 12字符 |
| 必须包含 | 大写字母+小写字母+数字+特殊字符 |
| 最大有效期 | 90天 |
| 历史密码 | 不可重复使用最近10个 |
| 锁定策略 | 5次失败锁定30分钟 |
| 传输加密 | 强制HTTPS |

### 15.3 认证安全

#### 15.3.1 多因素认证 (MFA)

```go
// 支持的MFA类型
type MFAType string

const (
    MFATotp   MFAType = "totp"    // TOTP (Google Authenticator)
    MFASms    MFAType = "sms"     // 短信验证码
    MFAEmail  MFAType = "email"   // 邮件验证码
    MFAWebauthn MFAType = "webauthn" // WebAuthn (FIDO2)
)

// TOTP验证
func VerifyTOTP(user *User, code string) bool {
    totpURL := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s",
        "MonitorSystem", user.Username, user.MFASecret, "MonitorSystem")
    
    key, err := totp.GenerateKey(totpURL)
    if err != nil {
        return false
    }
    
    return totp.Validate(code, key)
}
```

#### 15.3.2 会话管理

```go
// 会话配置
type SessionConfig struct {
    // Session有效期
    MaxAge         int           // 8小时 (28800秒)
    AbsoluteTimeout int          // 24小时 (86400秒)
    
    // Session安全
    Secure         bool          // 仅HTTPS传输
    HttpOnly       bool          // 禁止JS访问
    SameSite       string        // "strict" 或 "lax"
    
    // 并发控制
    MaxSessions    int           // 最多5个同时登录
    KillOthers     bool          // 登录时踢掉其他会话
}
```

### 15.4 API安全

#### 15.4.1 认证方式

| 方式 | 适用场景 | 说明 |
|------|----------|------|
| **JWT** | 前端API调用 | Bearer Token |
| **API Key** | 第三方集成 | Header: X-API-Key |
| **Service Account** | 服务间调用 | mTLS + Token |

#### 15.4.2 API限流

```go
// 限流配置
type RateLimitConfig struct {
    // 用户限流
    UserRequestsPerMinute  int           // 60
    UserRequestsPerHour   int           // 1000
    
    // IP限流
    IPRequestsPerMinute  int           // 100
    IPRequestsPerDay     int           // 10000
    
    // 特殊路径
    AuthEndpointsPerMinute int         // 5 (登录接口)
    
    // 限流响应
    RetryAfter          int             // 60秒
}
```

#### 15.4.3 API安全头

```
# 必需的安全响应头
Strict-Transport-Security: max-age=31536000; includeSubDomains
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Content-Security-Policy: default-src 'self'
X-Request-ID: <uuid>
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 59
X-RateLimit-Reset: 1234567890
```

### 15.5 数据安全

#### 15.5.1 敏感数据脱敏

| 数据类型 | 脱敏规则 |
|----------|----------|
| 密码 | BCrypt哈希存储 |
| API Key | SHA256哈希 |
| 手机号 | 138****1234 |
| 邮箱 | a***@example.com |
| IP地址 | 192.168.**.** |
| MAC地址 | 00:11:**:**:44:55 |

#### 15.5.2 数据加密

```go
// 敏感字段加密
type EncryptionConfig struct {
    Algorithm   string  // AES-256-GCM
    KeyPath     string  // /etc/ssl/keys/encryption.key
    IVLength    int     // 12
    TagLength   int     // 16
}

// 加密示例
func EncryptSensitive(data string, config *EncryptionConfig) (string, error) {
    key, _ := LoadKey(config.KeyPath)
    iv := GenerateIV(config.IVLength)
    
    block, err := aes.NewCipher(key)
    if err != nil {
        return "", err
    }
    
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }
    
    ciphertext := gcm.Seal(iv, iv, []byte(data), nil)
    
    // 存储格式: base64(iv + ciphertext)
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}
```

### 15.6 合规检查清单

| 检查项 | PCI-DSS | 等保2.0 | 说明 |
|--------|---------|---------|------|
| TLS 1.2+ | ✅ | ✅ | 禁用旧版本 |
| 强密码策略 | ✅ | ✅ | 最小12位 |
| MFA认证 | ✅ | ✅ | 关键操作 |
| 审计日志 | ✅ | ✅ | 1年保留 |
| 访问控制 | ✅ | ✅ | 最小权限 |
| 数据加密 | ✅ | ✅ | 敏感数据 |
| 漏洞扫描 | ✅ | ✅ | 季度扫描 |
| 渗透测试 | ✅ | ✅ | 年度测试 |
| 事件响应 | ✅ | ✅ | 24小时响应 |

---

## 十六、设备二维码扫码模块

### 16.1 功能概述

为每个设备生成唯一二维码标签，用于现场运维人员使用手机App扫码快速查看设备信息。

```
┌─────────────────────────────────────────────────────────────────────┐
│                     设备二维码扫码系统                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌──────────────┐                              ┌──────────────┐   │
│   │  后台系统    │                              │  手机App    │   │
│   │              │                              │              │   │
│   │  1.生成二维码 │                              │  3.扫码识别 │   │
│   │  2.打印标签  │────────── 粘贴 ────────────►  │  4.鉴权验证 │   │
│   │  3.管理码    │                              │  5.显示信息 │   │
│   └──────────────┘                              └──────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 16.2 使用场景

| 场景 | 说明 |
|------|------|
| **现场巡检** | 扫描设备二维码，快速查看设备状态、历史告警 |
| **故障处理** | 扫码获取设备详细信息，快速定位问题 |
| **资产管理** | 扫码核对资产信息，更新资产状态 |
| **信息查询** | 扫码查看IP配置、网络端口、维保信息 |
| **扫码签到** | 巡检签到，记录GPS位置和时间戳 |

### 16.3 二维码设计

#### 16.3.1 二维码内容

```
二维码编码格式: JSON Base64编码

{
  "t": "device",           // 类型: device/ticket
  "id": "uuid",            // 设备UUID
  "ts": 1700000000,        // 生成时间戳
  "s": "校验码",           // SHA256(secret + id + ts)
  "v": 1                   // 版本号
}

示例: 
{"t":"device","id":"550e8400-e29b-41d4-a716-446655440000","ts":1700000000,"s":"abc123","v":1}
```

#### 16.3.2 二维码参数

| 参数 | 说明 |
|------|------|
| **类型** | device(设备)/ticket(工单) |
| **ID** | 关联的资源UUID |
| **时间戳** | 生成时间，用于校验 |
| **校验码** | 防伪验证，防止伪造 |
| **版本** | 格式版本，兼容升级 |

#### 16.3.3 二维码规格

| 参数 | 值 |
|------|-----|
| **纠错级别** | M (15%损坏可识别) |
| **版本** | 自动调整 (1-40) |
| **尺寸** | 100-300px (打印尺寸: 2-5cm) |
| **颜色** | 黑底白码 / 白底黑码 |
| **Logo** | 可选中间加Logo |

### 16.4 权限分级显示

根据扫码用户的权限，动态展示不同深度的信息：

| 权限级别 | 可查看内容 | 适用角色 |
|----------|------------|----------|
| **Lv.1 基础** | 设备名称、资产编号、位置 | 访客、外包人员 |
| **Lv.2 标准** | + IP地址、网络端口、运行状态 | 普通运维 |
| **Lv.3 完整** | + 硬件配置、软件信息、告警历史 | 高级运维 |
| **Lv.4 全部** | + 维保信息、联系人、财务信息 | 管理员 |

### 16.5 功能详情

#### 16.5.1 二维码生成

```go
// 二维码生成服务
type QRCodeService struct {
    secret string  // 签名密钥
}

func (s *QRCodeService) GenerateDeviceQR(assetID string) ([]byte, error) {
    // 生成签名
    timestamp := time.Now().Unix()
    signature := s.generateSignature(assetID, timestamp)
    
    // 构建内容
    payload := QRPayload{
        Type:      "device",
        ID:        assetID,
        Timestamp: timestamp,
        Signature: signature,
        Version:   1,
    }
    
    content, _ := json.Marshal(payload)
    
    // 生成二维码图片
    qr, err := qrcode.New(base64.StdEncoding.EncodeToString(content), qrcode.Medium)
    if err != nil {
        return nil, err
    }
    
    return qr.PNG(300), nil
}
```

#### 16.5.2 扫码鉴权流程

```
扫码流程:

    手机App扫码
        │
        ▼
    ┌───────────────┐
    │ 解析二维码内容  │
    └───────┬───────┘
            │
            ▼
    ┌───────────────┐
    │ 校验签名      │ ◄── secret校验 + 时间戳校验
    └───────┬───────┘
            │
            ▼
    ┌───────────────┐
    │ 获取用户Token │ ◄── App登录
    └───────┬───────┘
            │
            ▼
    ┌───────────────┐
    │ 查询用户权限  │ ◄── RBAC权限
    └───────┬───────┘
            │
            ▼
    ┌───────────────┐
    │ 获取设备信息  │ ◄── 根据权限过滤
    └───────┬───────┘
            │
            ▼
    ┌───────────────┐
    │ 返回对应信息  │ ◄── Lv.1/Lv.2/Lv.3/Lv.4
    └───────────────┘
```

#### 16.5.3 离线支持

| 场景 | 方案 |
|------|------|
| **无网络** | 二维码含核心信息（名称+IP+端口），离线可显示 |
| **弱网络** | App本地缓存最近扫码记录 |
| **完全离线** | 预下载权限范围内设备信息包 |

### 16.6 标签打印

#### 16.6.1 标签模板

```html
<!-- 标准设备标签 (80x50mm) -->
<div class="qr-label" style="width:80mm; height:50mm;">
    <div class="qr-code">
        <img src="data:image/png;base64,..." />
    </div>
    <div class="label-info">
        <h3>{{asset_name}}</h3>
        <p>编号: {{asset_tag}}</p>
        <p>位置: {{idc_name}} - {{rack_name}}</p>
        <p class="qr-url">扫码查看详情</p>
    </div>
</div>
```

#### 16.6.2 标签类型

| 类型 | 尺寸 | 用途 |
|------|------|------|
| **标准标签** | 80×50mm | 服务器、网络设备 |
| **小型标签** | 40×30mm | 小型设备、配件 |
| **大型标签** | 100×70mm | 机柜、配线架 |
| **资产标签** | 60×40mm | 固定资产 |

### 16.7 数据库设计 - 二维码模块

```sql
-- 二维码记录表
CREATE TABLE qr_codes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID NOT NULL REFERENCES assets(id),
    
    -- 码信息
    code_type       VARCHAR(20) NOT NULL DEFAULT 'device',  -- device/ticket/other
    code_content    TEXT NOT NULL,                           -- 编码内容
    signature       VARCHAR(64) NOT NULL,                    -- 校验签名
    version         INTEGER DEFAULT 1,
    
    -- 生成信息
    generated_at    TIMESTAMP DEFAULT NOW(),
    generated_by    UUID REFERENCES users(id),
    
    -- 打印信息
    printed         BOOLEAN DEFAULT FALSE,
    printed_at      TIMESTAMP,
    printed_by     UUID,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active',  -- active/inactive/replaced
    expires_at      TIMESTAMP,                     -- 过期时间
    
    -- 关联标签
    label_template  VARCHAR(50),                    -- 标签模板
    print_count     INTEGER DEFAULT 0,             -- 打印次数
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 扫码记录表
CREATE TABLE qr_scan_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    qr_code_id      UUID NOT NULL REFERENCES qr_codes(id),
    
    -- 扫码信息
    scanned_at     TIMESTAMP DEFAULT NOW(),
    scanned_by     UUID REFERENCES users(id),
    
    -- 位置信息
    gps_lat         DECIMAL(10,8),
    gps_lng         DECIMAL(11,8),
    scan_location   VARCHAR(255),                  -- 扫码位置描述
    
    -- 客户端信息
    app_version     VARCHAR(20),
    device_type     VARCHAR(50),                    -- ios/android
    os_version      VARCHAR(50),
    
    -- 权限级别
    permission_level INTEGER DEFAULT 1,
    
    -- 返回信息
    info_returned   JSONB,                         -- 返回的信息字段
    
    -- 网络状态
    network_status  VARCHAR(20),                    -- online/offline
    response_time   INTEGER,                       -- 响应时间(ms)
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 标签模板表
CREATE TABLE label_templates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) NOT NULL,
    description     TEXT,
    
    -- 模板配置
    template_type   VARCHAR(50),                    -- qr/rfid/barcode
    width_mm        INTEGER NOT NULL,               -- 宽度(mm)
    height_mm       INTEGER NOT NULL,               -- 高度(mm)
    
    -- 模板内容
    template_html   TEXT NOT NULL,                  -- HTML模板
    css_style       TEXT,                           -- 样式
    variables       JSONB,                          -- 变量定义
    
    -- 打印设置
    paper_type      VARCHAR(50),                    -- 标签纸类型
    dpi             INTEGER DEFAULT 300,
    
    -- 状态
    is_default      BOOLEAN DEFAULT FALSE,
    enabled         BOOLEAN DEFAULT TRUE,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID REFERENCES users(id)
);

-- 权限级别配置表
CREATE TABLE qr_permission_levels (
    level           INTEGER NOT NULL,
    name            VARCHAR(50) NOT NULL,
    description     TEXT,
    
    -- 可查看字段
    visible_fields  JSONB NOT NULL,                -- 字段列表
    
    -- 可执行操作
    allowed_actions JSONB,                         -- 操作列表
    
    -- 角色关联
    role_ids        UUID[],                        -- 可访问此级别的角色
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    
    PRIMARY KEY (level)
);

-- 插入默认权限配置
INSERT INTO qr_permission_levels (level, name, visible_fields, allowed_actions) VALUES
(1, '基础', 
  '{"asset_name", "asset_tag", "idc_name", "rack_name"}'::jsonb,
  '{"view"}'::jsonb),
(2, '标准',
  '{"asset_name", "asset_tag", "idc_name", "rack_name", "ipv4_addresses", "network_interfaces", "status"}'::jsonb,
  '{"view", "check_status"}'::jsonb),
(3, '完整',
  '{"asset_name", "asset_tag", "idc_name", "rack_name", "ipv4_addresses", "network_interfaces", "status", "hardware_info", "software_info", "alert_history"}'::jsonb,
  '{"view", "check_status", "view_history"}'::jsonb),
(4, '全部',
  '{"*"}'::jsonb,
  '{"view", "check_status", "view_history", "update_info", "create_ticket"}'::jsonb);
```

### 16.8 API设计

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/v1/qr/generate/:assetId | 生成设备二维码 |
| GET | /api/v1/qr/:qrId | 获取二维码信息 |
| GET | /api/v1/qr/:qrId/image | 获取二维码图片 |
| POST | /api/v1/qr/scan | 扫码解析 (App调用) |
| GET | /api/v1/qr/:qrId/scan-logs | 扫码记录 |
| GET | /api/v1/qr/templates | 标签模板列表 |
| POST | /api/v1/qr/templates | 创建标签模板 |
| PUT | /api/v1/qr/print | 打印标签 |

#### 16.8.1 扫码API响应示例

```json
// 扫码返回 (Lv.2 权限)
{
  "code": 0,
  "data": {
    "asset": {
      "id": "uuid",
      "name": "Server-001",
      "tag": "ASSET-2024-001",
      "type": "server"
    },
    "location": {
      "idc": "北京数据中心A",
      "rack": "IDC-A-15",
      "position": "12U"
    },
    "network": {
      "interfaces": [
        {
          "name": "eth0",
          "ip": "192.168.1.100",
          "mac": "00:11:22:33:44:55",
          "status": "up"
        },
        {
          "name": "eth1",
          "ip": "10.0.0.100",
          "mac": "00:11:22:33:44:56",
          "status": "up"
        }
      ]
    },
    "status": {
      "health": "healthy",
      "last_check": "2025-07-18T10:30:00Z",
      "active_alerts": 0
    },
    "actions": ["view_details", "create_ticket", "check_history"]
  },
  "permissions": {
    "level": 2,
    "can_view_all": false,
    "can_update": false
  }
}
```

### 16.9 手机App设计

#### 16.9.1 App功能

| 功能 | 说明 |
|------|------|
| **扫码** | 相机扫码/相册识别 |
| **离线缓存** | 最近扫码记录离线查看 |
| **GPS签到** | 记录扫码位置 |
| **信息展示** | 根据权限展示设备信息 |
| **工单执行** | 接收/处理工单、上传照片 |
| **扫码历史** | 记录个人扫码轨迹 |

#### 16.9.2 App界面

```
┌─────────────────────┐
│ 🔍 扫码           ◄── 相机预览
├─────────────────────┤
│                     │
│    ┌───────────┐    │
│    │           │    │
│    │   [二维码] │    │
│    │           │    │
│    └───────────┘    │
│                     │
├─────────────────────┤
│ 📱 Server-001      │
│ 📍 北京A-15机柜    │
│ 📶 192.168.1.100   │
│ ✅ 状态: 正常      │
├─────────────────────┤
│ [详情] [历史] [工单]│
└─────────────────────┘
```

#### 16.9.3 工单执行功能

##### 16.9.3.1 工单列表

```
┌─────────────────────┐
│ 📋 我的工单 (3)    │
├─────────────────────┤
│                     │
│ 🔴 P1-紧急         │
│ [Server-01] CPU异常 │
│ 处理人: 张三        │
│ 截止: 14:00        │
│ ────────────────── │
│                     │
│ 🟡 P2-高           │
│ [Switch-A] 端口扩容 │
│ 处理人: 我         │
│ 截止: 明天 18:00   │
│ ────────────────── │
│                     │
│ 🟢 P3-中           │
│ [Firewall-B] 例行检查 │
│ 处理人: 我         │
│ 截止: 07-20       │
│                     │
├─────────────────────┤
│ [待处理] [处理中] [已完成]│
└─────────────────────┘
```

##### 16.9.3.2 工单详情与执行

```
┌─────────────────────┐
│ ◀ 返回        P2   │
├─────────────────────┤
│                     │
│ [Switch-A] 端口扩容 │
│                     │
│ 📝 描述:           │
│ 需要将Gi1/0/1端口  │
│ 从1G扩容至10G     │
│                     │
│ 📍 位置:            │
│ 北京A-15机柜 - 5U │
│                     │
│ 📋 步骤:           │
│ ① 确认新光模块到货 │
│ ② 备份当前配置    │
│ ③ 更换光模块      │
│ ④ 验证连通性      │
│ ⑤ 上传完工照片    │
│                     │
├─────────────────────┤
│ 📷 拍照上传        │ ◄── 点击上传照片
│ ┌───────────────┐  │
│ │  [照片1] ✅   │  │ ◄── 已上传
│ └───────────────┘  │
│ ┌───────────────┐  │
│ │  [照片2] ✅   │  │
│ └───────────────┘  │
│ ┌───────────────┐  │
│ │  [+ 添加照片] │  │ ◄── 继续添加
│ └───────────────┘  │
│                     │
├─────────────────────┤
│ 📝 处理说明:        │
│ ┌─────────────────┐│
│ │                 ││
│ │ (可输入文字)    ││
│ │                 ││
│ └─────────────────┘│
│                     │
├─────────────────────┤
│ [暂存] [提交工单]   │
└─────────────────────┘
```

##### 16.9.3.3 拍照功能

| 功能 | 说明 |
|------|------|
| **单张拍照** | 现场拍照上传 |
| **多张连拍** | 连续拍摄多张照片 |
| **照片标注** | 可添加文字说明 |
| **水印** | 自动添加时间、地点水印 |
| **离线存储** | 无网络时本地暂存 |
| **压缩上传** | 自动压缩减少流量 |
| **EXIF信息** | 保留GPS、时间戳 |

##### 16.9.3.4 照片水印示例

```
┌─────────────────────────┐
│ 照片 + 水印             │
│ ┌───────────────────┐   │
│ │                   │   │
│ │   [现场照片]      │   │
│ │                   │   │
│ │  ┌─────────────┐ │   │
│ │  │ 2025-07-18 │ │   │
│ │  │ 14:32:05   │ │   │
│ │  │ 北京A-15机柜 │ │   │
│ │  │ 张三        │ │   │
│ │  └─────────────┘ │   │
│ └───────────────────┘   │
└─────────────────────────┘
```

### 16.10 工单执行数据库设计扩展

```sql
-- 工单照片表
CREATE TABLE ticket_photos (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id),
    step_id         UUID REFERENCES ticket_workflows(id),  -- 关联步骤
    
    -- 文件信息
    file_name       VARCHAR(255) NOT NULL,
    file_path       VARCHAR(500) NOT NULL,
    file_size       BIGINT NOT NULL,                       -- 字节
    mime_type       VARCHAR(100) NOT NULL,
    
    -- 照片信息
    description     VARCHAR(500),
    
    -- EXIF信息
    gps_lat         DECIMAL(10,8),
    gps_lng         DECIMAL(11,8),
    taken_at        TIMESTAMP,                             -- 拍摄时间
    device_model    VARCHAR(100),                          -- 拍摄设备
    
    -- 水印
    has_watermark   BOOLEAN DEFAULT TRUE,
    watermark_text  VARCHAR(255),
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'uploading',      -- uploading/ready/failed
    
    -- 上传信息
    uploaded_at     TIMESTAMP DEFAULT NOW(),
    uploaded_by     UUID REFERENCES users(id),
    
    -- 元数据
    metadata        JSONB,                                 -- 扩展信息
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 工单步骤执行记录
CREATE TABLE ticket_step_executions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id),
    workflow_id     UUID NOT NULL REFERENCES ticket_workflows(id),
    
    -- 执行信息
    executor_id     UUID NOT NULL REFERENCES users(id),
    started_at      TIMESTAMP DEFAULT NOW(),
    completed_at    TIMESTAMP,
    
    -- 执行状态
    status          VARCHAR(20) DEFAULT 'in_progress',    -- pending/in_progress/completed/skipped
    result          VARCHAR(20),                           -- success/failed
    
    -- 执行详情
    remark          TEXT,
    photos          UUID[],                               -- 关联照片
    
    -- 签名/确认
    signature_data  TEXT,                                 -- 手写签名(base64)
    confirmed_by    UUID REFERENCES users(id),
    confirmed_at    TIMESTAMP,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 工单处理记录表
CREATE TABLE ticket_progress (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id       UUID NOT NULL REFERENCES tickets(id),
    
    -- 进度信息
    progress_type   VARCHAR(50) NOT NULL,                 -- photo/update/status/approve/comment
    
    -- 内容
    content         TEXT,
    photos          UUID[],                               -- 照片ID列表
    
    -- 位置
    gps_lat         DECIMAL(10,8),
    gps_lng         DECIMAL(11,8),
    location_desc   VARCHAR(255),
    
    -- 客户端信息
    app_version     VARCHAR(20),
    device_type     VARCHAR(50),
    os_version      VARCHAR(50),
    
    -- 时间
    recorded_at     TIMESTAMP DEFAULT NOW(),
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 离线同步记录表
CREATE TABLE offline_sync_queue (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    
    -- 同步类型
    sync_type       VARCHAR(50) NOT NULL,                 -- photo/ticket_progress/checkin
    
    -- 内容
    data_content    JSONB NOT NULL,
    media_files     JSONB,                                 -- 附件信息
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'pending',         -- pending/syncing/completed/failed
    retry_count     INTEGER DEFAULT 0,
    last_retry      TIMESTAMP,
    error_message   TEXT,
    
    -- 同步时间
    created_at      TIMESTAMP DEFAULT NOW(),
    synced_at       TIMESTAMP
);

-- 索引
CREATE INDEX idx_ticket_photos_ticket ON ticket_photos(ticket_id);
CREATE INDEX idx_ticket_photos_uploaded ON ticket_photos(uploaded_at);
CREATE INDEX idx_step_executions_ticket ON ticket_step_executions(ticket_id);
CREATE INDEX idx_step_executions_executor ON ticket_step_executions(executor_id);
CREATE INDEX idx_progress_ticket ON ticket_progress(ticket_id);
CREATE INDEX idx_progress_recorded ON ticket_progress(recorded_at DESC);
CREATE INDEX idx_offline_sync_user ON offline_sync_queue(user_id, status);
```

### 16.11 工单App API扩展

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/app/tickets | 获取我的工单列表 |
| GET | /api/v1/app/tickets/:id | 获取工单详情 |
| PUT | /api/v1/app/tickets/:id/accept | 接受工单 |
| PUT | /api/v1/app/tickets/:id/start | 开始处理 |
| PUT | /api/v1/app/tickets/:id/complete | 完成工单 |
| POST | /api/v1/app/tickets/:id/progress | 上报进度 |
| POST | /api/v1/app/tickets/:id/photos | 上传照片 |
| POST | /api/v1/app/tickets/:id/steps/:stepId/complete | 完成步骤 |
| POST | /api/v1/app/offline/sync | 离线数据同步 |
| GET | /api/v1/app/offline/queue | 获取待同步队列 |

#### 16.11.1 工单执行API示例

```json
// 开始处理工单
PUT /api/v1/app/tickets/{ticketId}/start
Request: {}
Response: {
  "code": 0,
  "data": {
    "ticket_id": "uuid",
    "status": "in_progress",
    "started_at": "2025-07-18T10:00:00Z",
    "current_step": {
      "id": "step-uuid",
      "name": "确认光模块",
      "description": "检查新光模块型号是否正确"
    }
  }
}

// 上报进度 (含照片)
POST /api/v1/app/tickets/{ticketId}/progress
Request: {
  "remark": "光模块已更换完成，正在验证连通性",
  "photos": [
    {
      "file_name": "photo1.jpg",
      "description": "更换前端口状态",
      "gps_lat": 39.9042,
      "gps_lng": 116.4074
    },
    {
      "file_name": "photo2.jpg", 
      "description": "更换后端口状态",
      "gps_lat": 39.9042,
      "gps_lng": 116.4074
    }
  ]
}
Response: {
  "code": 0,
  "data": {
    "progress_id": "uuid",
    "recorded_at": "2025-07-18T10:30:00Z"
  }
}

// 完成工单
PUT /api/v1/app/tickets/{ticketId}/complete
Request: {
  "result": "success",
  "summary": "端口扩容完成，已验证10G链路正常",
  "photos": ["photo1.jpg", "photo2.jpg", "photo3.jpg"],
  "signature_data": "data:image/png;base64,..."  // 可选手写签名
}
Response: {
  "code": 0,
  "data": {
    "ticket_id": "uuid",
    "status": "completed",
    "completed_at": "2025-07-18T11:00:00Z",
    "next_approval": {
      "required": true,
      "approver_id": "approver-uuid"
    }
  }
}
```

### 16.12 离线同步机制

#### 16.12.1 离线场景支持

| 场景 | 方案 |
|------|------|
| **完全离线** | 数据本地存储，网络恢复后自动上传 |
| **弱网络** | 优先上传文本，照片批量压缩上传 |
| **同步冲突** | 时间戳优先，冲突时提示用户确认 |

#### 16.12.2 离线数据队列

```
离线数据管理流程:

    1. 用户操作(拍照/填表)
              │
              ▼
    2. 数据存入本地SQLite
              │
              ▼
    3. 检测网络状态
         │
    ┌────┴────┐
    │         │
  在线      离线
    │         │
    ▼         ▼
  4.立即上传  5.加入离线队列
              │
              ▼
    6. 定期检测网络
              │
              ▼
    7. 网络恢复
              │
              ▼
    8. 按顺序上传
              │
              ▼
    9. 标记已同步
              │
              ▼
   10. 清理本地缓存
```

#### 16.12.3 离线同步配置

```go
// 离线同步配置
type OfflineSyncConfig struct {
    // 启用离线模式
    Enabled bool `default:"true"`
    
    // 最大缓存条目数
    MaxQueueSize int `default:"100"`
    
    // 照片压缩
    PhotoCompression struct {
        Enabled bool `default:"true"`
        Quality int `default:"70"`      // JPEG质量 0-100
        MaxWidth int `default:"1920"`  // 最大宽度
        MaxHeight int `default:":"1080"` // 最大高度
    }
    
    // 同步策略
    SyncStrategy struct {
        AutoSync bool `default:"true"`
        SyncInterval int `default:"30"` // 秒
        BatchPhotos bool `default:"true"`
        BatchSize int `default:"5"`    // 每次同步照片数
    }
    
    // 冲突处理
    ConflictResolution string `default:"server_wins"` // server_wins/client_wins/manual
}
```

### 16.10 安全性设计

#### 16.10.1 防伪校验

| 安全措施 | 说明 |
|----------|------|
| **签名验证** | SHA256签名防止篡改 |
| **时间戳校验** | 防止使用过期二维码 |
| **频率限制** | 同一二维码1分钟内只能扫5次 |
| **异常检测** | 异地扫码告警 |
| **IP限制** | 扫码请求来源验证 |

#### 16.10.2 安全策略

```go
// 扫码安全检查
func (s *QRCodeService) ValidateScan(scan *ScanRequest) error {
    // 1. 解析内容
    payload, err := ParseQRPayload(scan.Code)
    if err != nil {
        return ErrInvalidCode
    }
    
    // 2. 签名校验
    expectedSig := s.generateSignature(payload.ID, payload.Timestamp)
    if payload.Signature != expectedSig {
        return ErrInvalidSignature
    }
    
    // 3. 时间戳校验 (7天内有效)
    if time.Now().Unix() - payload.Timestamp > 7*86400 {
        return ErrCodeExpired
    }
    
    // 4. 查询二维码状态
    qrCode, _ := s.GetQRCodeByAssetID(payload.ID)
    if qrCode.Status != "active" {
        return ErrCodeInactive
    }
    
    // 5. 频率限制
    if s.IsRateLimited(payload.ID, scan.UserID) {
        return ErrTooFrequent
    }
    
    return nil
}
```

### 16.11 批量打印

```go
// 批量生成设备二维码
type BatchQRGenerateRequest struct {
    AssetIDs     []string       // 资产ID列表
    TemplateID   string         // 标签模板
    PrintCount   int            // 打印份数
   纸张类型       string         // 标签纸类型
}

func (s *QRCodeService) BatchGenerate(req BatchQRGenerateRequest) ([]byte, error) {
    var pdfBytes bytes.Buffer
    
    for _, assetID := range req.AssetIDs {
        qrImage, _ := s.GenerateDeviceQR(assetID)
        asset, _ := s.GetAsset(assetID)
        
        // 渲染标签到PDF
        s.RenderLabelToPDF(&pdfBytes, qrImage, asset, req.TemplateID)
    }
    
    return pdfBytes.Bytes(), nil
}
```

---


---

## 十二、钉钉小程序集成

### 12.1 集成概述

将运维监控平台以钉钉小程序的形式集成，利用钉钉的SSO认证、组织架构同步、消息推送等能力。

```
┌─────────────────────────────────────────────────────────────────────┐
│                   钉钉小程序集成架构                                   │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────┐                                              │
│   │   钉钉客户端    │                                              │
│   │                 │                                              │
│   │  ┌───────────┐ │    ┌───────────┐                          │
│   │  │运维小程序 │ │    │  机器人   │                          │
│   │  └─────┬─────┘ │    └─────┬─────┘                          │
│   │        │           │                                      │
│   └────────┼───────────┼────────────────────────────────────┘ │
│            │           │                                           │
│            ▼           ▼                                           │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │                    钉钉开放平台                           │  │
│   │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐   │  │
│   │  │ 认证API │  │ 组织API │  │ 消息API │  │ 审批API │   │  │
│   │  └─────────┘  └─────────┘  └─────────┘  └─────────┘   │  │
│   └─────────────────────────────────────────────────────────┘  │
│            │                                                         │
│            ▼                                                         │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │               运维监控平台后端                           │  │
│   │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐   │  │
│   │  │ 认证服务 │  │ 组织服务 │  │ 告警服务 │  │ 工单服务 │   │  │
│   │  └─────────┘  └─────────┘  └─────────┘  └─────────┘   │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 12.2 钉钉应用配置

```yaml
# dingtalk-config.yaml
# 钉钉集成配置

dingtalk:
  # 企业应用配置
  app:
    agent_id: "${DINGTALK_AGENT_ID}"
    app_key: "${DINGTALK_APP_KEY}"
    app_secret: "${DINGTALK_APP_SECRET}"
    
  # 小程序配置
  mini_app:
    app_id: "${DINGTALK_MINI_APP_ID}"
    app_secret: "${DINGTALK_MINI_APP_SECRET}"
    
  # 机器人配置
  robot:
    webhook: "${DINGTALK_ROBOT_WEBHOOK}"
    secret: "${DINGTALK_ROBOT_SECRET}"
    
  # 审批配置
  approval:
    process_code: "${DINGTALK_APPROVAL_PROCESS_CODE}"
    
  # 权限范围
  scope:
    - "corp"
    - "user"
    - "department"
    
  # 回调配置
  callback:
    url: "https://monitor.company.com/api/dingtalk/callback"
    token: "${DINGTALK_CALLBACK_TOKEN}"
    aes_key: "${DINGTALK_CALLBACK_AES_KEY}"
```

### 12.3 钉钉SSO认证

```go
// 钉钉SSO认证服务
type DingTalkAuthService struct {
    appKey    string
    appSecret string
    tokenCache *redis.Client
}

// 获取access_token
func (s *DingTalkAuthService) GetAccessToken() (string, error) {
    // 先从缓存获取
    token, err := s.tokenCache.Get("dingtalk:access_token").Result()
    if err == nil && token != "" {
        return token, nil
    }
    
    // 调用钉钉API获取
    url := fmt.Sprintf(
        "https://oapi.dingtalk.com/gettoken?appkey=%s&appsecret=%s",
        s.appKey, s.appSecret,
    )
    
    resp, err := http.Get(url)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    
    var result DingTalkTokenResponse
    json.NewDecoder(resp.Body).Decode(&result)
    
    if result.ErrCode != 0 {
        return "", fmt.Errorf("dingtalk error: %s", result.ErrMsg)
    }
    
    // 缓存token (有效期2小时)
    s.tokenCache.Set("dingtalk:access_token", result.AccessToken, 2*time.Hour)
    
    return result.AccessToken, nil
}

// 用户扫码登录
type DingTalkLoginRequest struct {
    Code string `json:"code"` // 临时授权码
}

type DingTalkLoginResponse struct {
    UserID      string `json:"userid"`
    Name        string `json:"name"`
    Avatar      string `json:"avatar"`
    Token       string `json:"token"`
    RefreshToken string `json:"refresh_token"`
}

func (s *DingTalkAuthService) Login(req *DingTalkLoginRequest) (*DingTalkLoginResponse, error) {
    // 1. 获取access_token
    accessToken, err := s.GetAccessToken()
    if err != nil {
        return nil, err
    }
    
    // 2. 通过code获取用户信息
    url := fmt.Sprintf(
        "https://oapi.dingtalk.com/user/getuserinfo?access_token=%s&code=%s",
        accessToken, req.Code,
    )
    
    resp, err := http.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    var userInfoResp DingTalkUserInfoResponse
    json.NewDecoder(resp.Body).Decode(&userInfoResp)
    
    if userInfoResp.ErrCode != 0 {
        return nil, fmt.Errorf("dingtalk error: %s", userInfoResp.ErrMsg)
    }
    
    // 3. 获取用户详情
    userDetail, err := s.GetUserDetail(accessToken, userInfoResp.UserID)
    if err != nil {
        return nil, err
    }
    
    // 4. 创建/更新本地用户
    user, err := s.SyncUser(userDetail)
    if err != nil {
        return nil, err
    }
    
    // 5. 生成JWT Token
    token, refreshToken, err := s.GenerateTokens(user)
    if err != nil {
        return nil, err
    }
    
    return &DingTalkLoginResponse{
        UserID:      userDetail.UserID,
        Name:        userDetail.Name,
        Avatar:      userDetail.Avatar,
        Token:       token,
        RefreshToken: refreshToken,
    }, nil
}

// 获取用户详情
func (s *DingTalkAuthService) GetUserDetail(accessToken, userID string) (*DingTalkUser, error) {
    url := fmt.Sprintf(
        "https://oapi.dingtalk.com/user/get?access_token=%s&userid=%s",
        accessToken, userID,
    )
    
    resp, err := http.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    var user DingTalkUser
    json.NewDecoder(resp.Body).Decode(&user)
    
    if user.ErrCode != 0 {
        return nil, fmt.Errorf("dingtalk error: %s", user.ErrMsg)
    }
    
    return &user, nil
}

// 同步用户到本地
func (s *DingTalkAuthService) SyncUser(dingUser *DingTalkUser) (*User, error) {
    // 查找或创建用户
    user, err := FindUserByDingTalkID(dingUser.UserID)
    if err != nil {
        return nil, err
    }
    
    if user == nil {
        user = &User{
            Username: dingUser.UserID,
            Nickname: dingUser.Name,
            Avatar:   dingUser.Avatar,
            // ...
        }
        user.DingTalkID = dingUser.UserID
        user.OrganizationIDs = []string{dingUser.Department[0]}
        err = CreateUser(user)
    } else {
        // 更新用户信息
        user.Nickname = dingUser.Name
        user.Avatar = dingUser.Avatar
        user.OrganizationIDs = dingUser.Department
        err = UpdateUser(user)
    }
    
    return user, err
}
```

### 12.4 组织架构同步

> 利用钉钉**通讯录（地址簿）**，同步组织架构和人员信息，减少移动端开发工作量。

#### 12.4.1 钉钉通讯录整合

```
┌─────────────────────────────────────────────────────────────────┐
│               钉钉通讯录整合架构                                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  钉钉通讯录                    本平台                            │
│  ┌─────────────┐              ┌──────────────┐                  │
│  │ 组织架构    │ ──────────▶ │ 组织架构同步  │                  │
│  │ (部门/员工) │   定时同步    │ (用户/角色)  │                  │
│  └─────────────┘              └──────────────┘                  │
│                                                                  │
│  ┌─────────────┐              ┌──────────────┐                  │
│  │ 设备管理员  │ ◀────────── │ 权限关联     │                  │
│  │ (通讯录标签) │ 标签映射     │ (按部门/标签)│                  │
│  └─────────────┘              └──────────────┘                  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

##### 通讯录同步内容

| 钉钉字段 | 同步到平台 | 说明 |
|----------|------------|------|
| 部门ID | organization.external_id | 组织ID |
| 部门名称 | organization.name | 部门名称 |
| 父部门ID | organization.parent_id | 层级关系 |
| 员工ID | user.external_id | 员工外部ID |
| 员工姓名 | user.nickname | 姓名 |
| 手机号 | user.phone | 登录用 |
| 邮箱 | user.email | 备用联系 |
| 部门 | user.department | 所属部门 |
| 标签 | user.tags | 设备管理标签 |

##### 设备管理员标签

利用钉钉**通讯录标签**标识设备管理人员：

```
钉钉通讯录标签：
├── 运维部-服务器管理员
├── 运维部-网络管理员
├── 运维部-安全管理员
└── 各部门IT对接人
```

映射到平台的**设备群组权限**：

```sql
-- 钉钉标签映射
CREATE TABLE dingtalk_label_mapping (
    id              UUID PRIMARY KEY,
    label_id        VARCHAR(50) NOT NULL,    -- 钉钉标签ID
    label_name      VARCHAR(100) NOT NULL,   -- 钉钉标签名
    
    -- 映射到平台
    org_id          UUID REFERENCES organizations(id),
    role_id         UUID REFERENCES roles(id),
    
    sync_enabled    BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);
```

#### 12.4.2 钉钉小程序功能设计

> 利用钉钉原生小程序框架，开发成本更低，还能使用钉钉的登录、消息等能力。

##### 小程序功能模块

| 功能 | 说明 | 对比 App |
|------|------|----------|
| **告警通知** | 实时推送，点击查看详情 | 相同 |
| **告警确认** | 快捷确认/处理告警 | 相同 |
| **工单处理** | 审批/处理工单 | 相同 |
| **资产查询** | 扫码/搜索查看资产 | 相同 |
| **值班查看** | 当前值班人员 | 特有 |
| **快速拨号** | 一键呼叫值班/负责人 | 特有 |
| **通讯录** | 直接调用钉钉通讯录 | 复用 |

##### 小程序页面设计

```
钉钉运维小程序页面：

┌─────────────────────────────────┐
│  👤 运维人员    [通知 3]       │
├─────────────────────────────────┤
│                                  │
│  ┌────────┐ ┌────────┐        │
│  │ 📊 仪表 │ │ 🔔 告警 │        │
│  │   盘   │ │   3条  │        │
│  └────────┘ └────────┘        │
│                                  │
│  ┌────────┐ ┌────────┐        │
│  │ 📝 工单 │ │ 📱 资产 │        │
│  │   5个  │ │  扫码  │        │
│  └────────┘ └────────┘        │
│                                  │
│  ┌────────┐ ┌────────┐        │
│  │ 👥 值班 │ │ 📞 呼叫 │        │
│  │ 查看   │ │ 值班  │        │
│  └────────┘ └────────┘        │
│                                  │
└─────────────────────────────────┘
```

##### 复用钉钉能力

| 钉钉能力 | 小程序复用方式 | 减少开发量 |
|----------|----------------|------------|
| **登录** | dd.login() 授权登录 | 免开发 |
| **通讯录** | dd.contactChoose() 选择联系人 | 免开发 |
| **消息** | dd.notify() 推送通知 | 免开发 |
| **电话** | dd.makePhoneCall() 拨打电话 | 免开发 |
| **扫码** | dd.scan() 扫码查资产 | 免开发 |
| **位置** | dd.getLocation | 免开发 |
| **支付** | 如有需要可接入 | 可选 |

```javascript
// 小程序调用钉钉原生能力示例

// 1. 登录授权
dd.login({
    success: (res) => {
        // 获取钉钉 userid，换取平台 token
    }
});

// 2. 选择联系人（呼叫值班）
dd.contactChoose({
    multiple: false,
    success: (res) => {
        // 选择值班人员，获取手机号
    }
});

// 3. 扫码查资产
dd.scan({
    type: 'qr',
    success: (res) => {
        // 扫描资产二维码，显示资产详情
    }
});

// 4. 拨打电话
dd.makePhoneCall({
    phoneNumber: '138xxxx8888'
});
```

##### 与平台数据对应

```sql
-- 钉钉用户映射
CREATE TABLE dingtalk_users (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id),
    
    -- 钉钉信息
    dingtalk_id     VARCHAR(50) NOT NULL,
    unionid         VARCHAR(50),
    mobile          VARCHAR(20),
    
    -- 同步信息
    last_sync_at    TIMESTAMP,
    is_active       BOOLEAN DEFAULT TRUE,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    
    UNIQUE(dingtalk_id)
);

-- 钉钉小程序配置
CREATE TABLE dingtalk_mini_app (
    id              UUID PRIMARY KEY,
    app_id          VARCHAR(50) NOT NULL,
    app_secret      VARCHAR(100),
    
    -- 消息模板
    alert_template_id VARCHAR(50),
    ticket_template_id VARCHAR(50),
    
    is_active       BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);
```

#### 12.4.3 开发工作量对比

| 对比项 | 独立 App | 钉钉小程序 | 节省 |
|----------|----------|------------|------|
| **登录模块** | 2周 | 复用钉钉 | 2周 |
| **通讯录** | 2周 | 复用钉钉 | 2周 |
| **消息推送** | 1周 | 复用钉钉 | 1周 |
| **扫码** | 0.5周 | 复用钉钉 | 0.5周 |
| **电话** | 0.5周 | 复用钉钉 | 0.5周 |
| **地图定位** | 1周 | 复用钉钉 | 1周 |
| **iOS/Android 适配** | 2周 | 无需 | 2周 |
| **应用市场上架** | 1周 | 无需 | 1周 |
| **总计** | **~10周** | **~3周** | **~7周** |

**结论**：开发钉钉小程序比独立 App **节省约 70% 工作量**！

```go
// 钉钉组织架构同步服务
type DingTalkOrgSyncService struct {
    accessToken string
    syncCache  *redis.Client
}

// 同步部门列表
func (s *DingTalkOrgSyncService) SyncDepartments() error {
    accessToken, err := s.GetAccessToken()
    if err != nil {
        return err
    }
    
    url := fmt.Sprintf(
        "https://oapi.dingtalk.com/department/list?access_token=%s",
        accessToken,
    )
    
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    var result DingTalkDeptListResponse
    json.NewDecoder(resp.Body).Decode(&result)
    
    for _, dept := range result.Department {
        // 同步每个部门
        org := &Organization{
            ExternalID:  dept.ID,
            Name:        dept.Name,
            ParentID:   dept.ParentID,
            SortOrder:   dept.Order,
            // ...
        }
        s.SyncOrganization(org)
    }
    
    return nil
}

// 同步部门成员
func (s *DingTalkOrgSyncService) SyncDepartmentMembers(deptID string) error {
    accessToken, err := s.GetAccessToken()
    if err != nil {
        return err
    }
    
    url := fmt.Sprintf(
        "https://oapi.dingtalk.com/user/list?access_token=%s&department_id=%s&size=100",
        accessToken, deptID,
    )
    
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    var result DingTalkUserListResponse
    json.NewDecoder(resp.Body).Decode(&result)
    
    for _, user := range result.UserList {
        // 同步每个用户
        s.SyncUser(&user)
    }
    
    return nil
}

// 全量同步
func (s *DingTalkOrgSyncService) FullSync() error {
    // 1. 同步部门
    if err := s.SyncDepartments(); err != nil {
        return err
    }
    
    // 2. 获取所有部门ID
    deptIDs := s.GetAllDepartmentIDs()
    
    // 3. 同步每个部门的成员
    for _, deptID := range deptIDs {
        if err := s.SyncDepartmentMembers(deptID); err != nil {
            logger.Warn("sync department %s failed: %v", deptID, err)
        }
    }
    
    // 4. 记录同步时间
    s.syncCache.Set("dingtalk:last_sync", time.Now().Unix(), 0)
    
    return nil
}
```

### 12.5 钉钉消息推送

```go
// 钉钉消息推送服务
type DingTalkMessageService struct {
    accessToken string
    robotWebhook string
    robotSecret string
}

// 发送工作通知消息
type DingTalkWorkMessage struct {
    MsgType  string `json:"msgtype"`
    Markdown *DingTalkMarkdownMessage `json:"markdown,omitempty"`
    Text     *DingTalkTextMessage `json:"text,omitempty"`
    Link     *DingTalkLinkMessage `json:"link,omitempty"`
    ActionCard *DingTalkActionCard `json:"actioncard,omitempty"`
}

func (s *DingTalkMessageService) SendWorkMessage(
    userIDs []string,
    msg *DingTalkWorkMessage,
) error {
    accessToken, err := s.GetAccessToken()
    if err != nil {
        return err
    }
    
    url := fmt.Sprintf(
        "https://oapi.dingtalk.com/topapi/message/corpconversation/asyncsend_v2?access_token=%s",
        accessToken,
    )
    
    body := map[string]interface{}{
        "agent_id": os.Getenv("DINGTALK_AGENT_ID"),
        "userid_list": strings.Join(userIDs, ","),
        "msg":        msg,
    }
    
    resp, err := http.PostJSON(url, body)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    var result DingTalkResponse
    json.NewDecoder(resp.Body).Decode(&result)
    
    if result.ErrCode != 0 {
        return fmt.Errorf("dingtalk error: %s", result.ErrMsg)
    }
    
    return nil
}

// 发送告警消息 (Markdown格式)
func (s *DingTalkMessageService) SendAlertMessage(
    userIDs []string,
    alert *Alert,
) error {
    msg := &DingTalkWorkMessage{
        MsgType: "markdown",
        Markdown: &DingTalkMarkdownMessage{
            Title: fmt.Sprintf("[%s] %s", alert.Level, alert.Title),
            Text: fmt.Sprintf(`## 告警通知
**级别**: %s
**设备**: %s
**内容**: %s
**时间**: %s

---
[查看详情](https://monitor.company.com/alerts/%s)
[确认告警](https://monitor.company.com/alerts/%s/ack)
[创建工单](https://monitor.company.com/tickets/create?alert_id=%s)`,
                alert.Level,
                alert.DeviceName,
                alert.Message,
                alert.CreatedAt.Format("2006-01-02 15:04:05"),
                alert.ID,
                alert.ID,
                alert.ID,
            ),
        },
    }
    
    return s.SendWorkMessage(userIDs, msg)
}

// 发送告警卡片消息
func (s *DingTalkMessageService) SendAlertActionCard(
    userIDs []string,
    alert *Alert,
) error {
    msg := &DingTalkWorkMessage{
        MsgType: "action_card",
        ActionCard: &DingTalkActionCard{
            Title:          fmt.Sprintf("[%s] %s", alert.Level, alert.Title),
            Markdown:       alert.Message,
            HideAvatar:     "0",
            btnOrientation:  "1",
            Buttons: []DingTalkActionCardButton{
                {
                    Title:     "查看详情",
                    ActionURL: fmt.Sprintf("dingtalk://monitoralert?alert_id=%s&action=view", alert.ID),
                },
                {
                    Title:     "确认告警",
                    ActionURL: fmt.Sprintf("dingtalk://monitoralert?alert_id=%s&action=ack", alert.ID),
                },
                {
                    Title:     "创建工单",
                    ActionURL: fmt.Sprintf("dingtalk://monitoralert?alert_id=%s&action=ticket", alert.ID),
                },
            },
        },
    }
    
    return s.SendWorkMessage(userIDs, msg)
}

// 机器人发送消息
func (s *DingTalkMessageService) SendRobotMessage(msg interface{}) error {
    timestamp := time.Now().UnixMilli()
    sign := s.generateSignature(timestamp)
    
    url := fmt.Sprintf(
        "%s&timestamp=%d&sign=%s",
        s.robotWebhook, timestamp, sign,
    )
    
    return http.PostJSON(url, msg)
}

// 生成签名
func (s *DingTalkMessageService) generateSignature(timestamp int64) string {
    stringToSign := fmt.Sprintf("%d\n%s", timestamp, s.robotSecret)
    h := hmac.New(sha256.New, []byte(s.robotSecret))
    h.Write([]byte(stringToSign))
    return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
```

### 12.6 钉钉审批集成

```go
// 钉钉审批服务
type DingTalkApprovalService struct {
    accessToken string
    processCode string
}

// 发起审批
func (s *DingTalkApprovalService) StartApproval(
    request *ApprovalRequest,
) (string, error) {
    accessToken, err := s.GetAccessToken()
    if err != nil {
        return "", err
    }
    
    url := fmt.Sprintf(
        "https://oapi.dingtalk.com/process/start?access_token=%s",
        accessToken,
    )
    
    body := map[string]interface{}{
        "process_code": s.processCode,
        "originator_user_id": request.InitiatorID,
        "dept_id": request.DeptID,
        "form_component_values": s.buildFormValues(request),
    }
    
    resp, err := http.PostJSON(url, body)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    
    var result DingTalkApprovalResponse
    json.NewDecoder(resp.Body).Decode(&result)
    
    if result.ErrCode != 0 {
        return "", fmt.Errorf("dingtalk error: %s", result.ErrMsg)
    }
    
    return result.ProcessInstanceID, nil
}

// 构建表单数据
func (s *DingTalkApprovalService) buildFormValues(request *ApprovalRequest) []map[string]interface{} {
    return []map[string]interface{}{
        {
            "name": "工单编号",
            "value": request.TicketNo,
        },
        {
            "name": "工单标题",
            "value": request.Title,
        },
        {
            "name": "工单类型",
            "value": request.TicketType,
        },
        {
            "name": "优先级",
            "value": request.Priority,
        },
        {
            "name": "描述",
            "value": request.Description,
        },
        {
            "name": "关联设备",
            "value": request.Devices,
        },
        {
            "name": "附件",
            "value": request.Attachments,
        },
    }
}

// 回调处理审批结果
func (s *DingTalkApprovalService) HandleCallback(callback *ApprovalCallback) error {
    switch callback.Type {
    case "start":
        // 审批开始
        return s.handleApprovalStart(callback)
    case "finish":
        // 审批完成
        return s.handleApprovalFinish(callback)
    case "cc":
        // 抄送
        return s.handleApprovalCC(callback)
    }
    return nil
}

// 处理审批完成
func (s *DingTalkApprovalService) HandleApprovalFinish(callback *ApprovalCallback) error {
    // 更新工单状态
    ticket, err := GetTicketByProcessInstanceID(callback.ProcessInstanceID)
    if err != nil {
        return err
    }
    
    if callback.Result == "agree" {
        ticket.Status = "approved"
        ticket.ApprovedAt = time.Now()
        ticket.ApprovedBy = callback.ApproverID
    } else {
        ticket.Status = "rejected"
        ticket.RejectedReason = callback.Remark
    }
    
    return UpdateTicket(ticket)
}
```

### 12.7 小程序页面设计

#### 12.7.1 页面结构

```
钉钉运维小程序
├── pages/
│   ├── home/                    # 首页
│   │   ├── index              # 仪表盘
│   │   ├── alerts             # 告警列表
│   │   └── quick-actions      # 快捷操作
│   │
│   ├── alerts/                 # 告警模块
│   │   ├── list               # 告警列表
│   │   ├── detail             # 告警详情
│   │   └── ack                # 告警确认
│   │
│   ├── tickets/                # 工单模块
│   │   ├── list               # 工单列表
│   │   ├── detail             # 工单详情
│   │   ├── create             # 创建工单
│   │   └── process            # 处理工单
│   │
│   ├── assets/                 # 资产模块
│   │   ├── list               # 资产列表
│   │   ├── scan               # 扫码查询
│   │   └── detail             # 资产详情
│   │
│   └── profile/                # 个人中心
│       ├── index              # 个人信息
│       ├── settings           # 设置
│       └── notification       # 通知设置
│
├── components/                  # 公共组件
│   ├── alert-card            # 告警卡片
│   ├── ticket-card            # 工单卡片
│   ├── device-card            # 设备卡片
│   └── status-badge           # 状态徽章
│
└── utils/                      # 工具函数
    ├── dingtalk.js            # 钉钉API封装
    ├── auth.js                # 认证
    └── api.js                 # API请求
```

#### 12.7.2 首页设计

```json
{
  "navigationBarTitleText": "运维监控",
  "usingComponents": {
    "alert-card": "/components/alert-card/index",
    "quick-action": "/components/quick-action/index"
  }
}
```

```json
{
  "data": {
    "stats": {
      "totalAlerts": 12,
      "criticalAlerts": 3,
      "warningAlerts": 5,
      "pendingTickets": 8
    },
    "recentAlerts": [],
    "myTickets": [],
    "quickActions": [
      {
        "id": "scan",
        "name": "扫码查询",
        "icon": "scan",
        "action": "navigateTo:/pages/assets/scan"
      },
      {
        "id": "create_ticket",
        "name": "创建工单",
        "icon": "edit",
        "action": "navigateTo:/pages/tickets/create"
      },
      {
        "id": "my_alerts",
        "name": "我的告警",
        "icon": "warning",
        "action": "navigateTo:/pages/alerts/list"
      },
      {
        "id": "my_tickets",
        "name": "我的工单",
        "icon": "task",
        "action": "navigateTo:/pages/tickets/list"
      }
    ]
  }
}
```

#### 12.7.3 告警详情页面

```json
{
  "navigationBarTitleText": "告警详情",
  
  "onLoad": function(options) {
    this.loadAlertDetail(options.alertId);
  },
  
  "methods": {
    "loadAlertDetail": function(alertId) {
      dd.showLoading({ title: '加载中...' });
      
      dd.request({
        url: '/api/v1/mobile/alerts/' + alertId,
        success: (res) => {
          this.setData({ alert: res.data });
        },
        complete: () => {
          dd.hideLoading();
        }
      });
    },
    
    "ackAlert": function() {
      dd.confirm({
        title: '确认告警',
        content: '确定要确认这个告警吗？',
        confirmButtonText: '确认',
        cancelButtonText: '取消',
        success: (result) => {
          if (result.confirm) {
            this.doAckAlert();
          }
        }
      });
    },
    
    "doAckAlert": function() {
      dd.request({
        url: '/api/v1/mobile/alerts/' + this.data.alert.id + '/ack',
        method: 'PUT',
        success: () => {
          dd.showToast({ type: 'success', content: '确认成功' });
          this.loadAlertDetail(this.data.alert.id);
        }
      });
    },
    
    "createTicket": function() {
      dd.navigateTo({
        url: '/pages/tickets/create?alert_id=' + this.data.alert.id
      });
    }
  }
}
```

### 12.8 数据库设计 - 钉钉集成

```sql
-- 钉钉配置表
CREATE TABLE dingtalk_config (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 应用配置
    agent_id        VARCHAR(50) NOT NULL,
    app_key         VARCHAR(100) NOT NULL,
    app_secret      VARCHAR(255) NOT NULL,
    
    -- 小程序配置
    mini_app_id     VARCHAR(50),
    mini_app_secret VARCHAR(255),
    
    -- 机器人配置
    robot_webhook   VARCHAR(500),
    robot_secret    VARCHAR(255),
    
    -- 审批配置
    approval_process_code VARCHAR(100),
    
    -- 状态
    enabled         BOOLEAN DEFAULT TRUE,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 钉钉用户映射表
CREATE TABLE dingtalk_user_mapping (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 映射关系
    dingtalk_user_id VARCHAR(100) NOT NULL UNIQUE,
    local_user_id   UUID NOT NULL REFERENCES users(id),
    
    -- 用户信息
    name            VARCHAR(100),
    avatar          VARCHAR(500),
    mobile          VARCHAR(20),
    department_ids  TEXT[],
    
    -- 同步状态
    last_sync      TIMESTAMP,
    sync_status     VARCHAR(20) DEFAULT 'synced',
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 部门映射表
CREATE TABLE dingtalk_department_mapping (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 映射关系
    dingtalk_dept_id VARCHAR(100) NOT NULL UNIQUE,
    local_org_id    UUID REFERENCES organizations(id),
    
    -- 部门信息
    name            VARCHAR(100),
    parent_id       VARCHAR(100),
    sort_order      INTEGER,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 消息发送记录表
CREATE TABLE dingtalk_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 消息信息
    msg_type        VARCHAR(50) NOT NULL,              -- work/robot/notification
    title           VARCHAR(255),
    content         TEXT,
    
    -- 发送信息
    sender_id       UUID REFERENCES users(id),
    recipient_ids   TEXT[],                              -- 钉钉userid列表
    sent_at         TIMESTAMP,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'pending',       -- pending/sent/failed
    dingtalk_msg_id VARCHAR(100),
    
    -- 错误信息
    error_code      VARCHAR(50),
    error_message   TEXT,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 审批记录表
CREATE TABLE dingtalk_approvals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 审批关联
    ticket_id       UUID REFERENCES tickets(id),
    process_instance_id VARCHAR(100),
    
    -- 发起信息
    initiator_id    UUID REFERENCES users(id),
    initiator_ding_id VARCHAR(100),
    
    -- 审批信息
    status          VARCHAR(20) NOT NULL,               -- pending/approved/rejected
    
    -- 审批节点
    current_approver VARCHAR(100),                      -- 钉钉userid
    approval_result VARCHAR(20),
    approval_remark TEXT,
    
    -- 抄送
    cc_users        TEXT[],
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

-- 同步历史表
CREATE TABLE dingtalk_sync_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 同步类型
    sync_type       VARCHAR(50) NOT NULL,              -- department/user/full
    
    -- 统计
    departments_created INTEGER DEFAULT 0,
    departments_updated INTEGER DEFAULT 0,
    users_created    INTEGER DEFAULT 0,
    users_updated    INTEGER DEFAULT 0,
    errors           INTEGER DEFAULT 0,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'running',
    started_at      TIMESTAMP DEFAULT NOW(),
    completed_at    TIMESTAMP,
    error_message   TEXT,
    
    duration_ms     INTEGER
);

-- 回调日志表
CREATE TABLE dingtalk_callback_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 回调信息
    callback_type   VARCHAR(50) NOT NULL,
    event_type      VARCHAR(100),
    payload         JSONB NOT NULL,
    
    -- 处理结果
    processed       BOOLEAN DEFAULT FALSE,
    process_result   TEXT,
    error_message   TEXT,
    
    -- 时间
    received_at     TIMESTAMP DEFAULT NOW(),
    processed_at    TIMESTAMP,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 索引
CREATE INDEX idx_dingtalk_user_mapping_ding ON dingtalk_user_mapping(dingtalk_user_id);
CREATE INDEX idx_dingtalk_user_mapping_local ON dingtalk_user_mapping(local_user_id);
CREATE INDEX idx_dingtalk_dept_mapping_ding ON dingtalk_department_mapping(dingtalk_dept_id);
CREATE INDEX idx_dingtalk_msg_status ON dingtalk_messages(status);
CREATE INDEX idx_dingtalk_approval_ticket ON dingtalk_approvals(ticket_id);
CREATE INDEX idx_dingtalk_callback_type ON dingtalk_callback_logs(callback_type);
```

### 12.9 API设计 - 钉钉集成

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/dingtalk/config | 获取钉钉配置 |
| POST | /api/v1/dingtalk/auth/login | 用户登录 |
| POST | /api/v1/dingtalk/auth/callback | 回调验证 |
| POST | /api/v1/dingtalk/sync/departments | 同步部门 |
| POST | /api/v1/dingtalk/sync/users | 同步用户 |
| POST | /api/v1/dingtalk/sync/full | 全量同步 |
| POST | /api/v1/dingtalk/messages/send | 发送消息 |
| POST | /api/v1/dingtalk/approval/start | 发起审批 |
| POST | /api/v1/dingtalk/approval/callback | 审批回调 |
| GET | /api/v1/dingtalk/sync/history | 同步历史 |

### 12.10 权限配置

```yaml
# 钉钉小程序权限配置
dingtalk_mini_app:
  # 小程序AppID
  app_id: "dingtalk_x_x_x"
  
  # 权限范围
  scope:
    - "dingtalk"         # 钉钉空间
    - "contact"           # 联系人
    - "device"           # 设备信息
    - "location"         # 位置信息
  
  # 功能权限
  features:
    - name: "alert_view"
      desc: "查看告警"
      required_scopes: ["contact"]
      
    - name: "alert_ack"
      desc: "确认告警"
      required_scopes: ["contact"]
      
    - name: "ticket_create"
      desc: "创建工单"
      required_scopes: ["contact"]
      
    - name: "ticket_process"
      desc: "处理工单"
      required_scopes: ["contact"]
      
    - name: "asset_scan"
      desc: "扫码查询"
      required_scopes: ["device"]
      
    - name: "asset_view"
      desc: "查看资产"
      required_scopes: ["contact"]
  
  # API权限
  api_whitelist:
    - "contact.userscope.getorg"
    - "contact.department.lists"
    - "message.corpconversation.asyncsend_v2"
    - "process.instance.start"
```

### 12.11 与现有权限系统集成

```
┌─────────────────────────────────────────────────────────────────────┐
│                   钉钉用户权限映射                                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   钉钉用户登录                                                       │
│        │                                                            │
│        ▼                                                            │
│   ┌─────────────────────────────────────────────┐                  │
│   │  1. 获取钉钉用户信息                        │                  │
│   │     - userid, name, department_ids         │                  │
│   └────────────────────┬────────────────────────┘                   │
│                        │                                             │
│                        ▼                                             │
│   ┌─────────────────────────────────────────────┐                  │
│   │  2. 映射到本地用户                           │                  │
│   │     - 根据userid查找映射记录                 │                  │
│   │     - 同步组织架构信息                       │                  │
│   └────────────────────┬────────────────────────┘                   │
│                        │                                             │
│                        ▼                                             │
│   ┌─────────────────────────────────────────────┐                  │
│   │  3. 获取本地权限                             │                  │
│   │     - 根据组织ID获取权限                     │                  │
│   │     - 根据角色获取权限                       │                  │
│   │     - 根据设备群组获取权限                   │                  │
│   └────────────────────┬────────────────────────┘                   │
│                        │                                             │
│                        ▼                                             │
│   ┌─────────────────────────────────────────────┐                  │
│   │  4. 合并权限并生成Token                     │                  │
│   └─────────────────────────────────────────────┘                  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

```go
// 钉钉用户权限服务
type DingTalkPermissionService struct {
    userMappingService *DingTalkUserMappingService
    orgService *OrganizationService
    permService *PermissionService
}

func (s *DingTalkPermissionService) GetUserPermissions(dingtalkUserID string) (*PermissionSet, error) {
    // 1. 获取用户映射
    mapping, err := s.userMappingService.GetByDingTalkID(dingtalkUserID)
    if err != nil {
        return nil, err
    }
    
    // 2. 获取组织权限
    orgPerms, err := s.orgService.GetOrganizationPermissions(mapping.LocalUserID)
    if err != nil {
        return nil, err
    }
    
    // 3. 获取角色权限
    rolePerms, err := s.GetRolePermissions(mapping.LocalUserID)
    if err != nil {
        return nil, err
    }
    
    // 4. 获取设备群组权限
    groupPerms, err := s.GetDeviceGroupPermissions(mapping.LocalUserID)
    if err != nil {
        return nil, err
    }
    
    // 5. 合并权限
    perms := &PermissionSet{
        RolePermissions: rolePerms,
        OrganizationPermissions: orgPerms,
        DeviceGroupPermissions: groupPerms,
    }
    
    return perms, nil
}
```

## 十七、后续工作
```

## 十七、后续工作

### 17.1 待细化内容

- [ ] 详细API接口文档 (OpenAPI/Swagger)
- [ ] 采集器SNMP OID清单
- [ ] 告警规则模板库
- [ ] 自动化运维脚本
- [ ] 备份恢复方案
- [ ] 性能基准测试
- [ ] 安全加固方案
- [ ] AI模块详细设计
- [ ] 工单流程配置化
- [ ] Splunk仪表盘模板
- [x] 二维码App原型设计
- [x] 批量打印服务优化
- [x] 离线同步机制设计
- [ ] App拍照功能详细设计
- [ ] 手写签名功能设计
- [x] 审批流程移动端适配
- [x] SolarWinds迁移工具详细实现
- [ ] 渐进式迁移切换方案
- [ ] 迁移数据一致性验证工具
- [x] VMware vCenter集成模块
- [ ] vCenter性能指标详细配置
- [ ] 虚拟机与宿主机关联可视化
- [x] 工单审核与多人协作模块
- [ ] 审核工作流引擎增强
- [ ] 绩效看板前端原型
- [x] 移动端告警通知模块
- [x] 组织架构与设备群组管理
- [ ] 组织权限API网关增强
- [ ] 移动端App原型设计
- [x] 设备群组与人员群组多对多权限关系设计
- [ ] 钉钉审批流程深度集成
- [ ] 钉钉机器人高级配置
- [ ] 多租户支持

### 17.2 分阶段实施

| 阶段 | 功能 | 周期 |
|------|------|------|
| Phase 1 | 基础框架 + 资产管理 | 4周 |
| Phase 2 | SNMP采集 + 监控 | 4周 |
| Phase 3 | 告警系统 + 通知 | 3周 |
| Phase 4 | 可视化(拓扑/机柜) | 3周 |
| Phase 5 | 自动发现 + Agent | 4周 |
| Phase 6 | 用户权限 + 审计 | 2周 |
| Phase 7 | 运维工单管理 | 2周 |
| Phase 8 | AI辅助分析 (基础) | 3周 |
| Phase 9 | 安全加固 + 合规 | 2周 |
| Phase 10 | 设备二维码模块 | 3周 |
| Phase 11 | App工单执行(照片) | 2周 |
| Phase 12 | SolarWinds迁移工具 | 3周 |
| Phase 13 | VMware vCenter集成 | 2周 |
| Phase 14 | 工单审核与多人协作 | 3周 |
| Phase 15 | 移动端告警与组织权限 | 3周 |
| Phase 16 | 多对多权限关系设计 | 2周 |
| Phase 17 | 优化 + 扩展 | 2周 |

---

*文档完 - v1.9 (2025-07-18)*
*最后更新: 2025-07-18 - 补充Weaviate多模态支持与LLM提供商扩展*

### 17.1 待细化内容

- [ ] 详细API接口文档 (OpenAPI/Swagger)
- [ ] 采集器SNMP OID清单
- [ ] 告警规则模板库
- [ ] 自动化运维脚本
- [ ] 备份恢复方案
- [ ] 性能基准测试
- [ ] 安全加固方案
- [ ] AI模块详细设计
- [ ] 工单流程配置化
- [ ] Splunk仪表盘模板
- [x] 二维码App原型设计
- [x] 批量打印服务优化
- [x] 离线同步机制设计
- [ ] App拍照功能详细设计
- [ ] 手写签名功能设计
- [ ] 审批流程移动端适配
- [x] SolarWinds迁移工具详细实现
- [ ] 渐进式迁移切换方案
- [ ] 迁移数据一致性验证工具
- [x] VMware vCenter集成模块
- [ ] vCenter性能指标详细配置
- [ ] 虚拟机与宿主机关联可视化
- [x] 工单审核与多人协作模块
- [ ] 审核工作流引擎增强
- [ ] 绩效看板前端原型
- [x] 移动端告警通知模块
- [x] 组织架构与设备群组管理

---

## 十三、多对多权限关系设计

### 13.1 设计理念

```
┌─────────────────────────────────────────────────────────────────────┐
│                 设备群组 ↔ 人员群组 多对多关系                         │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   设备群组 A ──────────────────┬───────────────────────────────┐   │
│         │                       │                               │   │
│         │                       │                               │   │
│         ▼                       ▼                               ▼   │
│   ┌─────────────┐       ┌─────────────┐       ┌─────────────┐   │
│   │ 运维部-管理员│       │ 运维部-值班 │       │ 业务部门A  │   │
│   │              │       │              │       │              │   │
│   │ 权限: 完整   │       │ 权限: 值班  │       │ 权限: 只读  │   │
│   └─────────────┘       └─────────────┘       └─────────────┘   │
│         │                       │                               │   │
│         │                       │                               │   │
│         ▼                       ▼                               ▼   │
│   设备群组 B ───────────────────────────────────────────────────   │
│         │                                                       │
│         │                                                       │
│         ▼                                                       │
│   ┌─────────────┐       ┌─────────────┐                        │
│   │ 运维部-管理员│       │ 项目团队B  │                        │
│   │              │       │              │                        │
│   │ 权限: 完整   │       │ 权限: 自定义 │                        │
│   └─────────────┘       └─────────────┘                        │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 13.2 灵活权限模型

#### 13.2.1 关系特点

| 关系类型 | 说明 | 示例 |
|----------|------|------|
| **一对多** | 一个设备群组对应多个人员群组 | 生产服务器 → 运维部+业务A+项目B |
| **多对一** | 多设备群组对应同一个人员群组 | 测试服务器+开发服务器 → 测试团队 |
| **多对多** | 交叉对应 | 运维部访问所有群组，测试团队访问部分 |

#### 13.2.2 权限可配置性

```
同一个设备群组(生产服务器)，不同人员群组访问权限不同：

运维部-管理员:     完整权限 (读写+告警+工单+审计)
运维部-值班人员:   值班期间权限 (读写+告警，限值班设备)
业务部门A:        只读权限 (查看+监控，无写入)
项目团队B:        自定义权限 (部分设备只读，部分可操作)
```

### 13.3 数据库设计增强版

```sql
-- ============================================================
-- 设备群组-人员群组关联表 (核心多对多关系表)
-- ============================================================
CREATE TABLE device_group_org_mapping (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 关联的群组
    device_group_id UUID NOT NULL REFERENCES device_groups(id),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    
    -- 关联类型
    mapping_type    VARCHAR(50) NOT NULL DEFAULT 'manual',  -- manual/auto/rule_based
    
    -- 关联规则 (如果是自动关联)
    match_conditions JSONB,  -- {"conditions": [{"field": "idc.name", "op": "eq", "value": "北京"}]}
    
    -- 优先级 (用于冲突解决，数字越大优先级越高)
    priority        INTEGER DEFAULT 0,
    
    -- 是否启用
    enabled         BOOLEAN DEFAULT TRUE,
    
    -- 描述
    description     TEXT,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID,
    
    UNIQUE (device_group_id, org_id)
);

-- 创建索引
CREATE INDEX idx_dg_org_mapping_device ON device_group_org_mapping(device_group_id);
CREATE INDEX idx_dg_org_mapping_org ON device_group_org_mapping(org_id);
CREATE INDEX idx_dg_org_mapping_enabled ON device_group_org_mapping(enabled);

-- ============================================================
-- 群组权限配置表 (设备群组与人员群组的详细权限)
-- ============================================================
CREATE TABLE group_permission_config (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 关联关系
    device_group_id UUID NOT NULL REFERENCES device_groups(id),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    
    -- 权限配置版本
    version         INTEGER DEFAULT 1,
    config_hash     VARCHAR(64),  -- 配置哈希，用于检测变更
    
    -- 详细权限配置
    permissions     JSONB NOT NULL,  -- 详细权限配置
    
    /* 权限配置结构示例:
    {
      "asset": {
        "read": {"enabled": true, "scope": "all"},
        "write": {"enabled": true, "scope": "own"},
        "delete": {"enabled": false},
        "export": {"enabled": true}
      },
      "monitor": {
        "read": {"enabled": true},
        "configure": {"enabled": false}
      },
      "alert": {
        "read": {"enabled": true},
        "ack": {"enabled": true},
        "create": {"enabled": false},
        "manage_rules": {"enabled": false}
      },
      "ticket": {
        "create": {"enabled": true, "scope": "own"},
        "read": {"enabled": true, "scope": "own_assigned"},
        "process": {"enabled": true, "scope": "assigned"},
        "approve": {"enabled": false},
        "audit_scope": {"enabled": true, "scope": "own"}
      },
      "audit": {
        "read": {"enabled": true, "scope": "own_actions"},
        "export": {"enabled": false}
      }
    }
    */
    
    -- 生效时间范围
    valid_from      TIMESTAMP,
    valid_to        TIMESTAMP,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active',  -- active/inactive/draft
    approved_by     UUID REFERENCES users(id),
    approved_at     TIMESTAMP,
    
    -- 变更历史
    change_reason   TEXT,
    previous_version INTEGER,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID,
    
    UNIQUE (device_group_id, org_id, version)
);

-- 创建索引
CREATE INDEX idx_gp_config_device ON group_permission_config(device_group_id);
CREATE INDEX idx_gp_config_org ON group_permission_config(org_id);
CREATE INDEX idx_gp_config_status ON group_permission_config(status);

-- ============================================================
-- 权限继承表 (群组间权限继承)
-- ============================================================
CREATE TABLE permission_inheritance (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 继承关系
    child_device_group_id UUID NOT NULL REFERENCES device_groups(id),
    parent_device_group_id UUID NOT NULL REFERENCES device_groups(id),
    
    -- 继承配置
    inherit_mode    VARCHAR(20) NOT NULL DEFAULT 'all',  -- all/none/custom
    
    -- 继承的权限
    inherited_permissions JSONB,  -- 继承的权限配置
    
    -- 覆盖的权限
    override_permissions JSONB,  -- 覆盖的权限
    
    -- 优先级
    priority        INTEGER DEFAULT 0,
    
    -- 状态
    enabled         BOOLEAN DEFAULT TRUE,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    created_by      UUID,
    
    UNIQUE (child_device_group_id, parent_device_group_id)
);

-- 创建索引
CREATE INDEX idx_perm_inherit_child ON permission_inheritance(child_device_group_id);
CREATE INDEX idx_perm_inherit_parent ON permission_inheritance(parent_device_group_id);

-- ============================================================
-- 设备-群组关联增强表 (支持多群组)
-- ============================================================
CREATE TABLE device_group_enhanced (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    device_group_id UUID NOT NULL REFERENCES device_groups(id),
    
    -- 关联属性
    is_primary      BOOLEAN DEFAULT FALSE,  -- 是否主群组
    is_manual       BOOLEAN DEFAULT FALSE,  -- 手动关联/自动匹配
    match_rule      JSONB,  -- 匹配规则
    
    -- 关联时间
    associated_at   TIMESTAMP DEFAULT NOW(),
    associated_by   UUID,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active',
    
    PRIMARY KEY (asset_id, device_group_id)
);

-- 创建索引
CREATE INDEX idx_dg_enhanced_asset ON device_group_enhanced(asset_id);
CREATE INDEX idx_dg_enhanced_group ON device_group_enhanced(device_group_id);
CREATE INDEX idx_dg_enhanced_primary ON device_group_enhanced(is_primary);

-- ============================================================
-- 人员-群组关联增强表 (支持多群组)
-- ============================================================
CREATE TABLE org_user_enhanced (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id          UUID NOT NULL REFERENCES organizations(id),
    
    -- 关联属性
    is_primary      BOOLEAN DEFAULT FALSE,  -- 是否主群组
    position        VARCHAR(100),  -- 职位
    title           VARCHAR(100),  -- 职称
    
    -- 权限继承
    inherit_group_perms BOOLEAN DEFAULT TRUE,  -- 继承群组权限
    custom_permissions JSONB,  -- 自定义权限 (覆盖群组权限)
    
    -- 关联时间
    joined_at       TIMESTAMP DEFAULT NOW(),
    left_at         TIMESTAMP,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'active',
    
    PRIMARY KEY (user_id, org_id)
);

-- 创建索引
CREATE INDEX idx_org_user_enhanced_user ON org_user_enhanced(user_id);
CREATE INDEX idx_org_user_enhanced_org ON org_user_enhanced(org_id);
CREATE INDEX idx_org_user_enhanced_primary ON org_user_enhanced(is_primary);

-- ============================================================
-- 权限变更历史表
-- ============================================================
CREATE TABLE permission_change_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- 变更信息
    config_id       UUID NOT NULL REFERENCES group_permission_config(id),
    change_type     VARCHAR(50) NOT NULL,  -- create/update/delete
    
    -- 变更内容
    before_config   JSONB,
    after_config   JSONB,
    change_reason   TEXT,
    
    -- 操作人
    operator_id     UUID NOT NULL REFERENCES users(id),
    operator_name   VARCHAR(100),
    
    -- 审批信息
    approved        BOOLEAN DEFAULT FALSE,
    approved_by     UUID REFERENCES users(id),
    approved_at     TIMESTAMP,
    approval_comment TEXT,
    
    -- 时间
    changed_at      TIMESTAMP DEFAULT NOW(),
    effective_from  TIMESTAMP,  -- 生效时间
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 创建索引
CREATE INDEX idx_perm_change_config ON permission_change_log(config_id);
CREATE INDEX idx_perm_change_operator ON permission_change_log(operator_id);
CREATE INDEX idx_perm_change_time ON permission_change_log(changed_at);

-- ============================================================
-- 有效权限视图 (计算用户的最终权限)
-- ============================================================
CREATE VIEW v_valid_permissions AS
SELECT 
    u.id as user_id,
    u.username,
    u.idc_id,
    dg.id as device_group_id,
    dg.name as device_group_name,
    
    -- 资产权限
    gpc.permissions->'asset'->'read'->>'enabled' as asset_read,
    gpc.permissions->'asset'->'write'->>'enabled' as asset_write,
    gpc.permissions->'asset'->'delete'->>'enabled' as asset_delete,
    
    -- 监控权限
    gpc.permissions->'monitor'->'read'->>'enabled' as monitor_read,
    gpc.permissions->'monitor'->'configure'->>'enabled' as monitor_config,
    
    -- 告警权限
    gpc.permissions->'alert'->'read'->>'enabled' as alert_read,
    gpc.permissions->'alert'->'ack'->>'enabled' as alert_ack,
    
    -- 工单权限
    gpc.permissions->'ticket'->'create'->>'enabled' as ticket_create,
    gpc.permissions->'ticket'->'read'->>'scope' as ticket_read_scope,
    gpc.permissions->'ticket'->'process'->>'scope' as ticket_process_scope,
    
    -- 审计权限
    gpc.permissions->'audit'->'read'->>'scope' as audit_read_scope,
    
    gpc.valid_from,
    gpc.valid_to,
    gpc.status
    
FROM users u
JOIN org_user_enhancedoue ON u.id =oue.user_id ANDoue.status = 'active'
JOIN device_group_org_mapping dgom ONoue.org_id = dgom.org_id AND dgom.enabled = true
JOIN device_groups dg ON dgom.device_group_id = dg.id
JOIN group_permission_config gpc ON dg.id = gpc.device_group_id 
    ANDoue.org_id = gpc.org_id 
    AND gpc.status = 'active'
    AND (gpc.valid_from IS NULL OR gpc.valid_from <= NOW())
    AND (gpc.valid_to IS NULL OR gpc.valid_to >= NOW());
```

### 13.4 权限计算引擎

```go
// 权限计算服务
type PermissionCalculator struct {
    db            *gorm.DB
    cache         *redis.Client
}

// 计算用户对某个设备群组的权限
func (c *PermissionCalculator) CalculateDeviceGroupPermission(
    userID, deviceGroupID uuid.UUID,
) (*PermissionSet, error) {
    
    // 1. 获取用户的所有组织关联
    userOrgs, err := c.getUserOrganizations(userID)
    if err != nil {
        return nil, err
    }
    
    // 2. 获取设备群组
    deviceGroup, err := c.getDeviceGroup(deviceGroupID)
    if err != nil {
        return nil, err
    }
    
    // 3. 获取直接关联的权限配置
    directPerms, err := c.getDirectPermissions(userOrgs, deviceGroupID)
    if err != nil {
        return nil, err
    }
    
    // 4. 获取继承的权限
    inheritedPerms, err := c.getInheritedPermissions(deviceGroupID)
    if err != nil {
        return nil, err
    }
    
    // 5. 获取用户的自定义权限 (覆盖)
    customPerms, err := c.getUserCustomPermissions(userID, deviceGroupID)
    if err != nil {
        return nil, err
    }
    
    // 6. 合并权限 (继承 -> 直接 -> 自定义)
    result := c.mergePermissions(inheritedPerms, directPerms, customPerms)
    
    return result, nil
}

// 获取用户的所有组织关联
func (c *PermissionCalculator) getUserOrganizations(userID uuid.UUID) ([]UserOrg, error) {
    var userOrgs []UserOrg
    err := c.db.Where("user_id = ? AND status = ?", userID, "active").
        Find(&userOrgs).Error
    return userOrgs, err
}

// 权限合并逻辑
func (c *PermissionCalculator) mergePermissions(
    inherited, direct, custom PermissionSet,
) PermissionSet {
    
    result := inherited
    
    // 直接权限覆盖继承
    for module, perms := range direct {
        if existing, ok := result[module]; ok {
            result[module] = c.mergeModulePermissions(existing, perms)
        } else {
            result[module] = perms
        }
    }
    
    // 自定义权限覆盖所有
    for module, perms := range custom {
        result[module] = c.mergeModulePermissions(result[module], perms)
    }
    
    return result
}

// 模块权限合并
func (c *PermissionCalculator) mergeModulePermissions(
    existing, override ModulePermission,
) ModulePermission {
    
    // 优先使用override的值
    result := existing
    
    // 合并字段
    if override.Read != nil {
        result.Read = override.Read
    }
    if override.Write != nil {
        result.Write = override.Write
    }
    if override.Delete != nil {
        result.Delete = override.Delete
    }
    if override.Scope != "" {
        result.Scope = override.Scope
    }
    if override.Conditions != nil {
        result.Conditions = override.Conditions
    }
    
    return result
}

// 获取用户最终权限 (合并所有群组)
func (c *PermissionCalculator) CalculateUserTotalPermissions(userID uuid.UUID) (*TotalPermission, error) {
    // 1. 获取用户所有群组
    userOrgs, err := c.getUserOrganizations(userID)
    if err != nil {
        return nil, err
    }
    
    totalPerm := &TotalPermission{
        ModulePermissions: make(map[string]ModulePermission),
    }
    
    // 2. 遍历所有群组，计算权限
    seenModules := make(map[string]bool)
    for _, uo := range userOrgs {
        // 获取该群组关联的所有设备群组
        deviceGroups, err := c.getOrgDeviceGroups(uo.OrgID)
        if err != nil {
            continue
        }
        
        // 3. 合并权限
        for _, dg := range deviceGroups {
            perm, err := c.CalculateDeviceGroupPermission(userID, dg.ID)
            if err != nil {
                continue
            }
            
            // 合并到总权限 (取并集)
            for module, modulePerm := range perm.ModulePermissions {
                if existing, ok := totalPerm.ModulePermissions[module]; ok {
                    // 权限合并：取并集
                    totalPerm.ModulePermissions[module] = c.mergeToTotal(existing, modulePerm, seenModules[module])
                } else {
                    totalPerm.ModulePermissions[module] = modulePerm
                }
                seenModules[module] = true
            }
        }
    }
    
    return totalPerm, nil
}
```

### 13.5 权限配置示例

#### 13.5.1 设备群组与人员群组关联配置

```yaml
# device-group-org-mapping.yaml
# 设备群组与人员群组关联配置

mappings:
  # 生产环境服务器群组
  - device_group: "生产环境服务器"
    orgs:
      # 运维部-管理员：完整权限
      - org: "运维部-管理员"
        priority: 100
        permissions:
          asset: {read: true, write: true, delete: true, export: true}
          monitor: {read: true, configure: true}
          alert: {read: true, ack: true, create: true, manage_rules: true}
          ticket: {create: true, read: {scope: "all"}, process: {scope: "all"}, approve: true}
          audit: {read: {scope: "all"}, export: true}
          
      # 运维部-值班人员：值班期间部分权限
      - org: "运维部-值班组"
        priority: 80
        valid_time: "shift_schedule"  # 按值班时间
        permissions:
          asset: {read: true, write: true, export: true}
          monitor: {read: true}
          alert: {read: true, ack: true, create: true}
          ticket: {create: true, read: {scope: "own_assigned"}, process: {scope: "assigned"}}
          audit: {read: {scope: "own_shift"}}
          
      # 业务部门A：只读权限
      - org: "业务部门A"
        priority: 50
        permissions:
          asset: {read: true}
          monitor: {read: true}
          alert: {read: true}
          ticket: {create: true, read: {scope: "own_created"}}
          audit: {read: {scope: "none"}}
          
      # 项目团队B：自定义权限
      - org: "项目团队B"
        priority: 60
        permissions:
          asset: {read: true}
          monitor: {read: true}
          alert: {read: true}
          ticket: {create: true, read: {scope: "own_project"}, process: false}
          audit: {read: {scope: "none"}}
          
  # 测试环境服务器群组
  - device_group: "测试环境服务器"
    orgs:
      # 运维部-管理员
      - org: "运维部-管理员"
        priority: 100
        permissions:
          asset: {read: true, write: true, delete: true}
          monitor: {read: true, configure: true}
          alert: {read: true, ack: true}
          ticket: {create: true, read: {scope: "all"}, process: {scope: "all"}}
          
      # 测试团队
      - org: "测试团队"
        priority: 90
        permissions:
          asset: {read: true, write: true}
          monitor: {read: true, configure: true}
          alert: {read: true, ack: true}
          ticket: {create: true, read: {scope: "all"}, process: {scope: "assigned"}}
          
      # 开发团队
      - org: "开发团队"
        priority: 50
        permissions:
          asset: {read: true}
          monitor: {read: true}
          alert: {read: true}
          ticket: {create: true}
```

#### 13.5.2 权限配置结构示例

```json
{
  "permissions": {
    "asset": {
      "read": {
        "enabled": true,
        "scope": "all",
        "conditions": null
      },
      "write": {
        "enabled": true,
        "scope": "own",
        "conditions": {
          "allow_fields": ["description", "tags", "custom_fields"]
        }
      },
      "delete": {
        "enabled": false,
        "reason": "删除权限需要审批"
      },
      "export": {
        "enabled": true,
        "scope": "all",
        "format": ["csv", "xlsx", "pdf"]
      }
    },
    "monitor": {
      "read": {
        "enabled": true,
        "scope": "all",
        "data_retention_days": 90
      },
      "configure": {
        "enabled": true,
        "scope": "own_groups"
      }
    },
    "alert": {
      "read": {
        "enabled": true,
        "scope": "assigned_groups"
      },
      "ack": {
        "enabled": true,
        "scope": "assigned_groups",
        "conditions": {
          "max_ack_count": 50
        }
      },
      "create": {
        "enabled": true,
        "scope": "all",
        "auto_create_ticket": false
      },
      "manage_rules": {
        "enabled": false
      }
    },
    "ticket": {
      "create": {
        "enabled": true,
        "scope": "all",
        "types": ["fault", "change", "maintenance"]
      },
      "read": {
        "enabled": true,
        "scope": "own_assigned",
        "project_scope": ["own"]
      },
      "process": {
        "enabled": true,
        "scope": "assigned",
        "allow_transfer": true
      },
      "approve": {
        "enabled": false,
        "approval_levels": []
      },
      "audit": {
        "enabled": true,
        "scope": "own_tickets",
        "export": true
      }
    },
    "audit": {
      "read": {
        "enabled": true,
        "scope": "own_actions",
        "time_range_days": 30
      },
      "export": {
        "enabled": false
      }
    }
  }
}
```

### 13.6 权限冲突解决策略

| 冲突场景 | 解决策略 |
|----------|----------|
| **同一用户多个群组** | 权限取并集 (union) |
| **权限冲突 (allow vs deny)** | deny优先覆盖allow |
| **不同群组的不同scope** | 合并scope，取并集 |
| **优先级冲突** | 优先级高的覆盖优先级低的 |
| **条件冲突** | 取交集 (更严格的条件) |

```go
// 权限冲突解决器
type ConflictResolver struct {
    // 冲突检测
    DetectConflicts(perms []PermissionSet) []Conflict
    
    // 冲突解决
    ResolveConflicts(conflicts []Conflict) PermissionSet
    
    // 优先级处理
    ApplyPriority(perms []PermissionSet) PermissionSet
}

// 冲突类型
type Conflict struct {
    Type        ConflictType
    Module      string
    Field       string
    Values      []interface{}
    Permissions []PermissionSource  // 来源
}

// 冲突解决示例
func (r *ConflictResolver) ResolveConflicts(conflicts []Conflict) PermissionSet {
    result := PermissionSet{}
    
    for _, conflict := range conflicts {
        switch conflict.Type {
        case "allow_deny":
            // deny覆盖allow
            result[conflict.Module] = r.applyDenyOverrides(conflict)
            
        case "scope_union":
            // scope取并集
            result[conflict.Module] = r.unionScopes(conflict)
            
        case "condition_intersect":
            // 条件取交集
            result[conflict.Module] = r.intersectConditions(conflict)
            
        case "priority_override":
            // 优先级覆盖
            result[conflict.Module] = r.applyPriority(conflict)
        }
    }
    
    return result
}
```

### 13.7 API设计增强

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/v1/device-groups/:id/orgs | 获取设备群组关联的人员群组 |
| POST | /api/v1/device-groups/:id/orgs | 关联人员群组 |
| PUT | /api/v1/device-groups/:id/orgs/:orgId/permissions | 配置权限 |
| DELETE | /api/v1/device-groups/:id/orgs/:orgId | 取消关联 |
| GET | /api/v1/orgs/:id/device-groups | 获取人员群组关联的设备群组 |
| GET | /api/v1/users/:id/permissions | 获取用户完整权限 |
| GET | /api/v1/users/:id/permissions/:deviceGroupId | 获取用户对特定设备群组的权限 |
| POST | /api/v1/permissions/check | 批量检查权限 |
| GET | /api/v1/permissions/audit-log | 权限变更审计日志 |

#### 13.7.1 权限检查API示例

```json
// 检查权限
POST /api/v1/permissions/check
Request: {
  "user_id": "uuid",
  "resource_type": "asset",  // asset/monitor/alert/ticket/audit
  "action": "write",
  "resource_id": "uuid",  // 可选
  "target_group_id": "uuid"  // 可选
}

Response: {
  "code": 0,
  "data": {
    "allowed": true,
    "source": {
      "org_id": "uuid",
      "org_name": "运维部-管理员",
      "device_group_id": "uuid",
      "device_group_name": "生产环境服务器",
      "permission": {
        "scope": "own",
        "conditions": null
      }
    },
    "applied_policies": [
      {"policy": "group_permission", "source": "运维部-管理员"},
      {"policy": "user_custom", "source": "自定义权限"}
    ]
  }
}
```

### 13.8 管理界面设计

#### 13.8.1 设备群组-人员群组关联界面

```
┌──────────────────────────────────────────────────────────────────────────┐
│  设备群组: 生产环境服务器                              [编辑] [删除]     │
├──────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  关联的人员群组 (5个)                                                    │
│  ┌────────────────────────────────────────────────────────────────┐   │
│  │  群组名称          │  优先级  │  资产  │  监控  │  告警  │ 工单 │  │
│  ├────────────────────────────────────────────────────────────────┤   │
│  │  运维部-管理员    │   100   │  完整  │ 完整  │ 完整  │ 完整  │   │
│  │  [权限详情] [编辑] [移除]                                         │   │
│  ├────────────────────────────────────────────────────────────────┤   │
│  │  运维部-值班组    │    80   │   读写  │ 只读  │ 读写  │ 处理  │   │
│  │  [权限详情] [编辑] [移除]                                         │   │
│  ├────────────────────────────────────────────────────────────────┤   │
│  │  业务部门A        │    50   │   只读  │ 只读  │ 只读  │ 创建  │   │
│  │  [权限详情] [编辑] [移除]                                         │   │
│  ├────────────────────────────────────────────────────────────────┤   │
│  │  项目团队B        │    60   │   只读  │ 只读  │ 只读  │ 创建  │   │
│  │  [权限详情] [编辑] [移除]                                         │   │
│  └────────────────────────────────────────────────────────────────┘   │
│                                                                          │
│  [+ 添加关联群组]                                                         │
│                                                                          │
├──────────────────────────────────────────────────────────────────────────┤
│  [保存配置]  [取消]                                                       │
└──────────────────────────────────────────────────────────────────────────┘
```

#### 13.8.2 权限配置弹窗

```
┌──────────────────────────────────────────────────────────────────────────┐
│  配置权限: 生产环境服务器 → 运维部-值班组                              │
├──────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  资产权限                                        │
│  [✓] 读取    范围: [所有设备 ▼]               │
│  [✓] 写入    范围: [所在群组 ▼]               │
│  [ ] 删除                                             │
│  [✓] 导出    格式: [CSV, XLSX ▼]              │
│                                                                          │
│  ───────────────────────────────────────────────                        │
│                                                                          │
│  监控权限                                        │
│  [✓] 读取                                            │
│  [ ] 配置                                            │
│                                                                          │
│  ───────────────────────────────────────────────                        │
│                                                                          │
│  告警权限                                        │
│  [✓] 读取                                            │
│  [✓] 确认    范围: [值班期间 ▼]               │
│  [✓] 创建                                            │
│  [ ] 管理规则                                        │
│                                                                          │
│  ───────────────────────────────────────────────                        │
│                                                                          │
│  工单权限                                        │
│  [✓] 创建    范围: [所有 ▼]                    │
│  [✓] 读取    范围: [我创建的 + 指派给我 ▼]     │
│  [✓] 处理    范围: [指派给我 ▼]               │
│  [ ] 审批                                            │
│  [✓] 审计    范围: [我处理的 ▼]               │
│                                                                          │
│  ───────────────────────────────────────────────                        │
│                                                                          │
│  生效时间                                                                 │
│  ○ 立即生效                                                               │
│  ○ 定时生效: [日期时间选择器]                                          │
│  ○ 按值班时间 (关联值班配置)                                            │
│                                                                          │
├──────────────────────────────────────────────────────────────────────────┤
│  [保存]  [取消]                                                           │
└──────────────────────────────────────────────────────────────────────────┘
```

- [ ] 组织权限API网关增强
- [ ] 移动端App原型设计

### 17.2 分阶段实施

| 阶段 | 功能 | 周期 |
|------|------|------|
| Phase 1 | 基础框架 + 资产管理 | 4周 |
| Phase 2 | SNMP采集 + 监控 | 4周 |
| Phase 3 | 告警系统 + 通知 | 3周 |
| Phase 4 | 可视化(拓扑/机柜) | 3周 |
| Phase 5 | 自动发现 + Agent | 4周 |
| Phase 6 | 用户权限 + 审计 | 2周 |
| Phase 7 | 运维工单管理 | 2周 |
| Phase 8 | AI辅助分析 (基础) | 3周 |
| Phase 9 | 安全加固 + 合规 | 2周 |
| Phase 10 | 设备二维码模块 | 3周 |
| Phase 11 | App工单执行(照片) | 2周 |
| Phase 12 | SolarWinds迁移工具 | 3周 |
| Phase 13 | VMware vCenter集成 | 2周 |
| Phase 14 | 工单审核与多人协作 | 3周 |
| Phase 15 | 移动端告警与组织权限 | 3周 |
| Phase 16 | 多对多权限关系设计 | 2周 |
| Phase 17 | 优化 + 扩展 | 2周 |

---

*文档完 - v1.9 (2025-07-18)*
*最后更新: 2025-07-18 - 补充Weaviate多模态支持与LLM提供商扩展*

---

## 十八、优化建议汇总

> 以下是针对本设计文档的优化建议，已整合至上述各章节

### 18.1 监控指标增强

| 建议增加的指标类型 | 说明 | 优先级 |
|-------------------|------|--------|
| **容器监控** | K8s Pod 状态、容器资源限制、Service 探针状态 | 高 |
| **SSL证书监控** | 证书过期时间自动监控 | 高 |
| **日志关键字** | 支持正则匹配关键字告警 | 中 |
| **业务指标** | 支持自定义埋点上报 | 中 |

### 18.2 告警策略增强

| 功能 | 说明 | 优先级 |
|------|------|--------|
| **告警关联分析** | 如"CPU高 + 磁盘IO高 + 网络异常" → 自动归类为同一故障 | 高 |
| **告警自动收敛** | 同一设备多个指标异常，只发一条主告警 | 高 |
| **告警自愈** | 支持触发 HTTP 回调执行预设脚本（如自动重启服务） | 中 |

### 18.3 资产管理增强

| 功能 | 说明 | 优先级 |
|------|------|--------|
| **配置变更追踪** | 定期对比设备配置，发现变更自动记录 | 高 |
| **IPAM 功能** | IP 地址段管理、地址分配、冲突检测 | 中 |
| **维保到期提醒** | 提前 30/15/7 天自动提醒 | 高 |

### 18.4 权限与安全

| 功能 | 说明 | 优先级 |
|------|------|--------|
| **RBAC 细粒度权限** | 资产按机房/部门隔离 | 高 |
| **审计日志** | 谁在什么时候操作了什么 | 高 |
| **备份恢复方案** | 定期备份 + 恢复演练 | 高 |

### 18.5 扩展性设计

| 功能 | 说明 | 优先级 |
|------|------|--------|
| **插件化架构** | 采集协议、告警渠道、发现方式做成插件 | 中 |
| **OpenAPI** | 供其他系统调用 | 中 |
| **Prometheus Exporter** | 暴露指标供 Prometheus 采集 | 中 |
| **Webhook 入库** | 支持被动接收其他系统推送的监控数据 | 低 |

#### 18.5.1 Zabbix 接入模块

对于已有 Zabbix 监控系统的企业，本平台支持 **数据接入** 和 **告警同步**，实现统一运维视图。

##### 接入模式

| 模式 | 说明 | 适用场景 |
|------|------|----------|
| **指标同步** | 从 Zabbix API 拉取历史数据到本平台 | 统一存储、历史分析 |
| **实时推送** | Zabbix 通过 Webhook 推送告警到本平台 | 统一告警处理 |
| **资产同步** | 同步 Zabbix 主机到本平台资产库 | 资产统一管理 |
| **完整迁移** | 历史数据 + 告警 + 资产全量迁移 | 替换 Zabbix |

##### Zabbix 接入配置

```sql
-- Zabbix 接入配置表
CREATE TABLE zabbix_connections (
    id                  UUID PRIMARY KEY,
    name                VARCHAR(100) NOT NULL,       -- 连接名称
    
    -- Zabbix 连接信息
    api_url             VARCHAR(500) NOT NULL,        -- https://zabbix.example.com/api
    api_user            VARCHAR(100),
    api_token           VARCHAR(255),                 -- 加密存储
    
    -- 同步配置
    sync_enabled        BOOLEAN DEFAULT TRUE,
    sync_assets         BOOLEAN DEFAULT TRUE,         -- 同步资产
    sync_metrics        BOOLEAN DEFAULT FALSE,        -- 同步指标
    sync_triggers       BOOLEAN DEFAULT TRUE,        -- 同步告警
    
    -- 采集配置
    metric_filter       JSONB,                        -- 要同步的指标过滤
    history_days        INTEGER DEFAULT 30,           -- 历史数据保留天数
    refresh_interval    INTEGER DEFAULT 300,          -- 刷新间隔(秒)
    
    -- 状态
    status              VARCHAR(20) DEFAULT 'active',
    last_sync_at        TIMESTAMP,
    last_error          TEXT,
    
    created_by          UUID REFERENCES users(id),
    created_at          TIMESTAMP DEFAULT NOW(),
    updated_at          TIMESTAMP
);

-- Zabbix 主机映射表
CREATE TABLE zabbix_host_mapping (
    id                  UUID PRIMARY KEY,
    connection_id       UUID REFERENCES zabbix_connections(id),
    
    -- Zabbix 侧信息
    zabbix_host_id     BIGINT NOT NULL,
    zabbix_host_name   VARCHAR(255),
    zabbix_group_ids   BIGINT[],
    
    -- 本平台侧信息
    asset_id            UUID REFERENCES assets(id),
    
    -- 同步状态
    sync_status         VARCHAR(20) DEFAULT 'pending',  -- pending/synced/error
    last_sync_at        TIMESTAMP,
    
    UNIQUE(connection_id, zabbix_host_id)
);

-- Zabbix 告警映射表
CREATE TABLE zabbix_alert_mapping (
    id                  UUID PRIMARY KEY,
    connection_id       UUID REFERENCES zabbix_connections(id),
    
    -- Zabbix 侧
    zabbix_trigger_id  BIGINT,
    zabbix_event_id    BIGINT,
    
    -- 本平台侧
    alert_id           UUID REFERENCES alerts(id),
    
    -- 同步信息
    sync_direction     VARCHAR(20),    -- zabbix_to_platform / bidirectional
    last_sync_at      TIMESTAMP,
    
    UNIQUE(connection_id, zabbix_event_id)
);
```

##### 数据同步流程

```
┌─────────────────────────────────────────────────────────────────┐
│                     Zabbix 数据同步流程                          │
├
│                                                                  │
─────────────────────────────────────────────────────────────────┤│  定时任务 (每5分钟)                                               │
│       │                                                          │
│       ▼                                                          │
│  遍历所有活跃连接 ──▶ 验证 API Token                              │
│       │                                                          │
│       ▼                                                          │
│  ┌──────────────────────────────────────────┐                   │
│  │  1. 资产同步                              │                   │
│  │     Zabbix Hosts ──▶ 资产匹配/创建         │                   │
│  │     └─ 主机名/IP/MAC 作为匹配键            │                   │
│  │                                          │                   │
│  │  2. 指标同步 (可选)                       │                   │
│  │     History API ──▶ 本平台 metrics 表      │                   │
│  │     └─ 按 itemid 映射到 asset_id          │                   │
│  │                                          │                   │
│  │  3. 告警同步                             │                   │
│  │     Problems API ──▶ 本平台 alerts 表       │                   │
│  │     └─ Trigger → 告警规则匹配              │                   │
│  └──────────────────────────────────────────┘                   │
│       │                                                          │
│       ▼                                                          │
│  更新同步状态 + 记录错误                                         │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

##### Zabbix 告警统一处理

```
Zabbix 告警事件
      │
      ▼
本平台 Webhook 接收
      │
      ├──▶ 告警去重 (基于 zabbix_event_id)
      │
      ├──▶ 关联资产 (通过 host_mapping)
      │
      ├──▶ 触发本平台告警规则 (可选二次判断)
      │
      └──▶ 统一通知渠道发送
```

##### 接入步骤

1. **添加 Zabbix 连接**
   - 填写 API URL、用户名、Token
   - 测试连接

2. **配置同步范围**
   - 选择同步资产/指标/告警
   - 设置指标过滤（如只同步 CPU/内存）

3. **执行同步**
   - 首次全量同步
   - 后续增量同步

4. **验证数据**
   - 对比数量、抽样验证

---

### 18.6 数据存储高可用

| 组件 | 高可用方案 | 优先级 |
|------|------------|--------|
| **PostgreSQL** | 主从复制 + 自动切换 | 高 |
| **TimescaleDB** | 分布式 + 冷热数据分离 | 高 |
| **Redis** | Cluster 模式或哨兵 | 中 |
| **MongoDB** | Replica Set 自动选主 | 中 |

> **注**：应用层部署于 VMware vSphere HA 集群，物理主机故障时自动迁移，无需自行构建主备模式（详见 3.2 部署架构）

### 18.7 核心功能优先级划分（推荐实施顺序）

| 优先级 | 阶段 | 核心功能 | 预期周期 |
|--------|------|----------|----------|
| **P0** | V1.0 | 资产管理（CRUD）+ PostgreSQL 核心表结构 + 基础用户认证 | 2-3周 |
| **P0** | V1.0 | SNMP 采集（基础指标）+ TimescaleDB 时序数据存储 | 2-3周 |
| **P0** | V1.0 | 基础告警（阈值触发）+ 通知渠道（钉钉/邮件） | 2周 |
| **P1** | V1.1 | 自动发现（网段扫描 + SNMP 发现） | 2周 |
| **P1** | V1.1 | 拓扑图（自动生成 + 手动编辑） | 2周 |
| **P1** | V1.2 | 可视化（仪表盘 + 历史趋势图） | 2周 |
| **P1** | V1.2 | 机柜可视化（2D/3D 渲染） | 2周 |
| **P2** | V2.0 | VMware vCenter 集成 | 1-2周 |
| **P2** | V2.0 | SolarWinds 迁移工具 | 2-3周 |
| **P2** | V2.1 | 权限管理（RBAC + 审计日志） | 2周 |
| **P2** | V2.1 | 运维工单管理 | 2周 |
| **P3** | V3.0 | 容器监控（K8s） | 2-3周 |
| **P3** | V3.0 | AI 辅助分析（异常检测 + 根因分析） | 3-4周 |
| **P3** | V3.1 | 插件化扩展 + OpenAPI | 2周 |

### 18.8 技术风险与应对

| 风险点 | 建议 |
|--------|------|
| SNMP 大规模采集性能 | 先做小规模压测，评估是否需要分布式采集 |
| TimescaleDB 磁盘空间 | 制定合理的冷热数据分离策略（近期数据保留高精度，历史数据降采样或归档） |
| 告警风暴 | 实现告警抑制、合并、收敛机制 |
| 迁移数据一致性 | 开发数据校验工具，迁移前后双跑对比 |

---

*优化建议已整合 - v1.10 (2025-02-13)*

---

## 十九、安全与抗攻击设计

> 本章节针对网络监控系统的安全性进行全面设计，涵盖传输安全、身份认证、访问控制、数据保护、入侵检测、安全审计等方面。

### 19.1 整体安全架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                         安全防护体系                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │                     边界安全                                 │   │
│   │   WAF / API 网关防火墙 / DDoS 防护 / IP 白名单              │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                  │                                  │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │                     认证授权                                 │   │
│   │   OAuth2 + JWT / MFA / RBAC / API 密钥                      │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                  │                                  │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │                     应用安全                                 │   │
│   │   输入验证 / SQL 注入防护 / XSS 防护 / CSRF Token           │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                  │                                  │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │                     数据安全                                 │   │
│   │   TLS 传输加密 / 敏感数据加密 / 密钥管理 (Vault)            │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                  │                                  │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │                     安全审计                                 │   │
│   │   操作日志 / 审计追踪 / 异常告警 / 合规报告                   │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 19.2 传输层安全

#### 19.2.1 TLS 加密

| 场景 | 要求 |
|------|------|
| **HTTPS (Web)** | TLS 1.3 强制，TLS 1.2 最低支持 |
| **API 通信** | 双向 TLS (mTLS) |
| **数据库连接** | TLS 加密 |
| **Agent 通信** | mTLS 或预共享密钥 |
| **内部服务** | Service Mesh (Istio) 自动 mTLS |

```yaml
# TLS 配置示例
tls:
  # 对外服务
  web:
    min_version: "1.3"
    cipher_suites:
      - "TLS_AES_256_GCM_SHA384"
      - "TLS_CHACHA20_POLY1305_SHA256"
    
  # Agent 双向认证
  agent:
    enabled: true
    client_cert_required: true
    
  # 证书管理
  cert_manager:
    provider: "letsencrypt"  # 或 internal CA
    auto_renewal: true
    renewal_days_before: 30
```

#### 19.2.2 网络隔离

| 区域 | 说明 |
|------|------|
| **DMZ 区** | API 网关、WAF |
| **应用区** | 业务服务（仅内网访问） |
| **数据区** | 数据库（仅应用区访问） |
| **管理区** | 运维管理（严格限制 IP） |

### 19.3 身份认证与访问控制

#### 19.3.1 认证机制

> **本平台采用 Web 界面登录，不涉及钉钉小程序登录。**

##### Web 登录认证流程

```
┌─────────────────────────────────────────────────────────────┐
│                   Web 登录认证流程                             │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  用户访问 ──▶ 登录页面 ──▶ 输入用户名/密码                    │
│                        │                                     │
│                        ▼                                     │
│               验证用户名/密码                                  │
│                        │                                     │
│              ┌────────┴────────┐                            │
│              │                 │                            │
│           密码错误           密码正确                         │
│              │                 │                             │
│              ▼                 ▼                             │
│         返回错误           检查是否启用 2FA                   │
│              │                 │                             │
│              ├────────┬────────┤                             │
│              │        │        │                             │
│          不启用      启用      │                             │
│              │        │        │                             │
│              ▼        ▼        ▼                             │
│          登录成功   输入验证码  ▼                             │
│                     (TOTP)   验证码错误                       │
│                        │        │                            │
│                        ▼        ▼                            │
│                    登录成功   返回错误                         │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

##### 认证方式

| 认证方式 | 适用场景 | 安全级别 |
|----------|----------|----------|
| **用户名 + 密码** | 基础登录（单因素） | 低 |
| **用户名 + 密码 + TOTP** | Web 登录（强制 2FA） | 高 |
| **密码 + 短信验证码** | 敏感操作（可选） | 中 |
| **密码 + 邮箱验证码** | 敏感操作（可选） | 中 |
| **OAuth2 / OIDC** | SSO 单点登录 | 高 |
| **API Key** | 第三方集成 | 中 |
| **JWT + 刷新 Token** | API 访问 | 高 |
| **硬件密钥 (YubiKey)** | 高管/运维（可选） | 极高 |

##### 2FA (双因素认证) 实现

- **默认方式**：TOTP（Time-based One-Time Password）
- **支持 App**：
  - Google Authenticator
  - FreeOTP
  - Microsoft Authenticator
  - 其他兼容 TOTP 的 App
- **首次登录**：扫描二维码绑定，后续每次登录输入 6 位动态码
- **可选增强**：硬件密钥（YubiKey）、短信/邮箱验证码

```yaml
# 2FA 配置
mfa:
  # 强制启用角色
  required_for:
    - role: "admin"        # 管理员
    - role: "operator"      # 运维人员
    - role: "auditor"      # 审计员
      
  # 推荐方式
  recommended:
    - type: "totp"         # TOTP (默认)
      icon: "google-authenticator"
    - type: "webauthn"     # 硬件密钥
      icon: "yubikey"
      
  # 可选方式
  optional:
    - type: "sms"          # 短信验证码
    - type: "email"        # 邮箱验证码
```

##### TOTP 绑定流程

```
首次绑定（用户设置 2FA）：
     │
     ▼
进入"安全设置" ──▶ 开启 2FA
     │
     ▼
系统生成密钥 + 二维码
     │
     ▼
用户用 Authenticator App 扫描
     │
     ▼
输入App显示的6位验证码确认
     │
   ┌─┴─┐
 成功 失败
     │
     ▼
绑定成功，保存加密后的密钥
```

#### 19.3.2 API 认证设计

```go
// API 认证中间件
func AuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 1. 检查 API Key
        apiKey := c.GetHeader("X-API-Key")
        if apiKey != "" {
            validateAPIKey(c, apiKey)
            return
        }
        
        // 2. 检查 JWT
        authHeader := c.GetHeader("Authorization")
        if strings.HasPrefix(authHeader, "Bearer ") {
            token := strings.TrimPrefix(authHeader, "Bearer ")
            validateJWT(c, token)
            return
        }
        
        // 3. 检查 OAuth Token
        oauthToken := c.GetHeader("X-OAuth-Token")
        if oauthToken != "" {
            validateOAuth(c, oauthToken)
            return
        }
        
        c.AbortWithStatus(401)
    }
}
```

#### 19.3.3 RBAC 权限模型

| 角色 | 权限 |
|------|------|
| **超级管理员** | 系统配置、用户管理、全部资产 |
| **运维管理员** | 资产管理、监控配置、工单审批 |
| **运维人员** | 执行工单、查看资产、填写进度 |
| **只读用户** | 查看资产、查看监控 |
| **审计员** | 查看日志、审计报告（无写入） |
| **API 用户** | 按分配的 API Key 权限 |

```sql
-- 权限表设计
CREATE TABLE permissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource        VARCHAR(50) NOT NULL,           -- assets/alerts/tickets/metrics
    action          VARCHAR(20) NOT NULL,           -- create/read/update/delete
    scope           VARCHAR(20) DEFAULT 'own',      -- own/department/all
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 角色权限表
CREATE TABLE role_permissions (
    role_id         UUID NOT NULL REFERENCES roles(id),
    permission_id   UUID NOT NULL REFERENCES permissions(id),
    
    PRIMARY KEY (role_id, permission_id)
);

-- 用户角色表
CREATE TABLE user_roles (
    user_id         UUID NOT NULL REFERENCES users(id),
    role_id         UUID NOT NULL REFERENCES roles(id),
    department_id   UUID,                           -- 部门隔离
    expires_at      TIMESTAMP,                     -- 临时角色
    
    PRIMARY KEY (user_id, role_id)
);
```

#### 19.3.4 资产隔离

```yaml
# 资产数据隔离配置
data_isolation:
  # 按部门隔离
  department_enabled: true
  
  # 按机房隔离
  idc_enabled: true
  
  # 权限继承
  inherit_to_children: true
  
  # 脱敏规则
  masking:
    - field: "sn"          # 序列号
      rule: "show_last_4"  # 只显示后4位
    - field: "password"
      rule: "always_hide"
    - field: "api_key"
      rule: "show_first_4"
```

### 19.4 应用安全

#### 19.4.1 输入验证与防护

| 防护类型 | 实现方式 |
|----------|----------|
| **SQL 注入** | 参数化查询，禁止字符串拼接 |
| **XSS** | 输出转义 + Content Security Policy |
| **CSRF** | Token 验证 + SameSite Cookie |
| **路径遍历** | 路径规范化 + 白名单 |
| **命令注入** | 禁止直接执行用户输入的命令 |
| **JSON 注入** | 严格 JSON 解析 |
| **正则 DoS** | 超时限制 + 预编译正则 |

```go
// 输入验证示例
func ValidateInput(input *UserInput) error {
    // 1. 基础验证
    if err := validate.Struct(input); err != nil {
        return err
    }
    
    // 2. 长度限制
    if len(input.Username) > 50 || len(input.Username) < 3 {
        return errors.New("username length must be 3-50")
    }
    
    // 3. 格式验证
    if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(input.Username) {
        return errors.New("username can only contain alphanumeric and underscore")
    }
    
    // 4. SQL 注入特征检测
    dangerousPatterns := []string{
        "';", "--", "/*", "xp_", "sp_", "exec", "execute"
    }
    for _, pattern := range dangerousPatterns {
        if strings.Contains(strings.ToLower(input.Username), pattern) {
            return errors.New("invalid characters in username")
        }
    }
    
    return nil
}
```

#### 19.4.2 API 安全

| 防护措施 | 说明 |
|----------|------|
| **限流** | 按用户/IP/接口限流，防止 DoS |
| **防刷** | 连续异常请求自动封禁 |
| **输入长度** | Body 最大 1MB，字符串最大长度限制 |
| **响应压缩** | 防止 CRIME/BREACH 攻击 |
| **API 版本** | 强制使用最新 API 版本 |

```yaml
# API 安全配置
api_security:
  rate_limit:
    # 按用户限流
    user:
      requests: 100
      window: "1m"       # 每分钟100次
      
    # 按 IP 限流
    ip:
      requests: 1000
      window: "1m"
      
    # 敏感操作限流
    sensitive:
      requests: 10
      window: "1m"
      
  # 防刷配置
  brute_force:
    max_failures: 5
    lockout_duration: "15m"
    reset_after: "1h"
    
  # 请求限制
  limits:
    max_body_size: "1MB"
    max_string_length: 10000
    max_array_length: 1000
    max_depth: 10
```

#### 19.4.3 Web 安全头

```go
// 安全中间件
func SecurityHeaders() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 防止 XSS
        c.Header("X-XSS-Protection", "1; mode=block")
        
        // 防止点击劫持
        c.Header("X-Frame-Options", "DENY")
        
        // MIME 类型 sniffing
        c.Header("X-Content-Type-Options", "nosniff")
        
        // 内容安全策略
        c.Header("Content-Security-Policy", 
            "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
        
        // 引用策略
        c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
        
        // 权限策略
        c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
        
        c.Next()
    }
}
```

### 19.5 数据安全

#### 19.5.1 敏感数据加密

| 数据类型 | 加密方式 | 说明 |
|----------|----------|------|
| **用户密码** | bcrypt/argon2 | 单向哈希 |
| **API Key** | AES-256-GCM | 存储加密 |
| **数据库密码** | AES-256-GCM | 存储加密 |
| **SNMP Community** | AES-256-GCM | 存储加密 |
| **SSH 密钥** | AES-256-GCM | 存储加密 |
| **日志敏感字段** | 动态脱敏 | 信用卡/身份证等 |

```go
// 敏感数据加密
type Encryptor struct {
    key []byte
}

func (e *Encryptor) Encrypt(plaintext string) (string, error) {
    // 生成随机 IV
    iv := make([]byte, 12)
    if _, err := rand.Read(iv); err != nil {
        return "", err
    }
    
    // AES-GCM 加密
    block, err := aes.NewCipher(e.key)
    if err != nil {
        return "", err
    }
    
    aesgcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }
    
    ciphertext := aesgcm.Seal(nil, iv, []byte(plaintext), nil)
    
    // 返回 IV + 密文
    return base64.StdEncoding.EncodeToString(append(iv, ciphertext...)), nil
}
```

#### 19.5.2 密钥管理

| 方案 | 说明 |
|------|------|
| **HashiCorp Vault** | 生产环境推荐，密钥管理金标准 |
| **AWS Secrets Manager** | 云环境 |
| **Kubernetes Secrets** | 容器环境 |
| **环境变量** | 开发/测试环境 |

```yaml
# Vault 集成配置
vault:
  address: "https://vault.company.com:8200"
  auth_method: "kubernetes"
  role: "network-monitor"
  
  # 密钥路径映射
  secrets:
    - path: "secret/data/db/credentials"
      env: "DB_PASSWORD"
    - path: "secret/data/api/keys"
      env: "API_KEYS"
    - path: "secret/data/snmp/community"
      env: "SNMP_COMMUNITY"
      
  # 自动轮换
  auto_rotate:
    enabled: true
    interval: "90d"
```

#### 19.5.3 数据脱敏

```go
// 数据脱敏函数
func MaskSensitiveData(data string, dataType string) string {
    switch dataType {
    case "phone":
        // 手机号: 138****1234
        if len(data) == 11 {
            return data[:3] + "****" + data[7:]
        }
    case "id_card":
        // 身份证: 110101****12345678
        if len(data) == 18 {
            return data[:6] + "********" + data[14:]
        }
    case "email":
        // 邮箱: a***@example.com
        parts := strings.Split(data, "@")
        if len(parts) == 2 && len(parts[0]) > 2 {
            return parts[0][:1] + "***" + parts[0][len(parts[0])-1:] + "@" + parts[1]
        }
    case "ip":
        // IP: 192.168.***.***
        parts := strings.Split(data, ".")
        if len(parts) == 4 {
            return parts[0] + "." + parts[1] + ".***.***"
        }
    }
    return data
}
```

### 19.6 入侵检测与防御

#### 19.6.1 异常行为检测

| 监控项 | 检测规则 |
|----------|----------|
| **异常登录** | 新设备/新IP/异地登录 |
| **暴力破解** | 连续5次登录失败 |
| **权限提升** | 普通用户尝试访问管理员API |
| **数据异常** | 大量数据导出/删除 |
| **API 滥用** | 异常高频请求 |

```yaml
# 异常行为检测规则
security_rules:
  login:
    - name: "异地登录"
      condition: "city != last_login_city && distance > 500km"
      action: "alert + MFA"
      
    - name: "暴力破解"
      condition: "fail_count > 5 in 10m"
      action: "block_ip + alert"
      
  api:
    - name: "数据批量导出"
      condition: "export_count > 1000 in 1h"
      action: "require_approval + alert"
      
    - name: "异常请求模式"
      condition: "request_pattern == 'scanning'"
      action: "block_ip"
```

#### 19.6.2 WAF 规则

```yaml
# WAF 配置
waf:
  rules:
    # SQL 注入
    - id: 1001
      name: "SQL Injection"
      pattern: "(?i)(union.*select|insert.*into|delete.*from|drop.*table)"
      action: "block"
      
    # XSS
    - id: 1002
      name: "XSS Attack"
      pattern: "(?i)(<script|javascript:|onerror=)"
      action: "block"
      
    # 路径遍历
    - id: 1003
      name: "Path Traversal"
      pattern: "(\.\./|\.\.\\)"
      action: "block"
      
    # 命令注入
    - id: 1004
      name: "Command Injection"
      pattern: "(?i)(;|\\||`|$)"
      action: "block"
      
  # 误报白名单
  whitelist:
    - path: "/api/v1/metrics"
      bypass_rules: [1004]  # 某些指标可能包含特殊字符
```

### 19.7 安全审计

#### 19.7.1 审计日志

| 日志类型 | 记录内容 | 保留期限 |
|----------|----------|----------|
| **登录日志** | 用户、IP、设备、时间、结果 | 1年 |
| **操作日志** | 谁、何时、操作了什么 | 1年 |
| **API 日志** | 请求者、路径、参数、响应码 | 6个月 |
| **安全日志** | 异常检测、封禁、告警 | 1年 |
| **错误日志** | 系统错误、异常堆栈 | 3个月 |

```json
// 审计日志格式
{
  "timestamp": "2025-07-18T10:30:00Z",
  "event_type": "ticket.update",
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "user_ip": "10.0.1.100",
  "user_agent": "Mozilla/5.0...",
  "resource": "ticket",
  "resource_id": "TKT-20250718-001",
  "action": "update",
  "changes": {
    "status": ["running", "completed"],
    "progress": [60, 100]
  },
  "result": "success",
  "risk_level": "low"
}
```

#### 19.7.2 合规报告

| 报告类型 | 生成频率 | 内容 |
|----------|----------|------|
| **访问报告** | 每周 | 登录统计、异常登录 |
| **权限报告** | 每月 | 权限变更、权限矩阵 |
| **数据访问** | 每月 | 敏感数据访问记录 |
| **安全事件** | 实时 | 攻击事件、处理情况 |
| **合规自查** | 每季度 | 等保自查清单 |

### 19.8 采集器安全

#### 19.8.1 Agent 安全

| 安全措施 | 说明 |
|----------|------|
| **mTLS 双向认证** | Agent 和服务端双向验证证书 |
| **预共享密钥** | 轻量环境可用 PSK |
| **签名验证** | 每个请求带签名，防止篡改 |
| **IP 白名单** | 只允许白名单 IP 连接 |
| **敏感数据本地处理** | 避免敏感数据在网络传输 |

```yaml
# Agent 安全配置
agent:
  security:
    # 认证方式
    auth:
      method: "mtls"        # mtls/psk/jwt
      cert_file: "/etc/monitor/agent.crt"
      key_file: "/etc/monitor/agent.key"
      ca_file: "/etc/monitor/ca.crt"
      
    # 请求签名
    signing:
      enabled: true
      algorithm: "hmac-sha256"
      secret_key: "${AGENT_SECRET}"
      
    # 网络限制
    network:
      allowed_server_ips:
        - "10.0.1.0/24"
      bind_interface: "eth0"
      
    # 本地数据保护
    local_data:
      encrypt: true
      auto_delete_after: "7d"
```

#### 19.8.2 SNMP 安全

```yaml
# SNMP 安全配置
snmp:
  # 认证
  v3:
    auth_protocol: "sha256"    # sha1/sha256
    priv_protocol: "aes256"    # des/aes128/aes256
    
  # 白名单
  allowed_devices:
    - "10.0.1.0/24"
    - "192.168.0.0/16"
    
  # 采集限速
  rate_limit:
    max_devices_per_scan: 100
    interval_between_devices: "100ms"
```

### 19.9 安全检查清单

#### 19.9.1 开发安全

- [ ] 所有 API 必须认证
- [ ] 敏感数据必须加密存储
- [ ] 禁止 SQL 字符串拼接
- [ ] 用户输入必须验证
- [ ] 错误信息不能泄露敏感信息
- [ ] 密码必须哈希存储
- [ ] 日志不能记录敏感信息

#### 19.9.2 部署安全

- [ ] 生产环境关闭调试模式
- [ ] 使用 TLS 1.3
- [ ] 启用 WAF
- [ ] 配置 IP 白名单
- [ ] 限制 API 请求频率
- [ ] 启用审计日志
- [ ] 定期更换密钥
- [ ] 备份加密

#### 19.9.3 运维安全

- [ ] 定期安全扫描
- [ ] 定期渗透测试
- [ ] 监控异常行为
- [ ] 及时更新安全补丁
- [ ] 定期审计日志
- [ ] 备份恢复演练

### 19.10 安全技术选型汇总

| 安全领域 | 推荐方案 |
|----------|----------|
| **API 网关** | Kong / APISIX / AWS API Gateway |
| **WAF** | ModSecurity / 腾讯云 WAF / AWS WAF |
| **身份认证** | Keycloak / Auth0 / OAuth2 |
| **密钥管理** | HashiCorp Vault / AWS Secrets Manager |
| **日志审计** | ELK + Audit Beat / Splunk |
| **入侵检测** | Wazuh / Suricata |
| **DDoS 防护** | CloudFlare / AWS Shield |
| **容器安全** | Falco / Clair / Trivy |

---

## 二十、运维保障体系设计

> 作为专业运维团队，我们需要考虑的不仅是功能实现，更是长期的运维保障。本章节涵盖SLA管理、备份恢复、值班值守、容量规划等运维关键领域。

### 20.1 SLA 服务等级管理

#### 20.1.1 SLA 目标定义

| 指标 | SLA 级别 S1 | SLA 级别 S2 | SLA 级别 S3 |
|------|-------------|-------------|-------------|
| **可用性** | ≥99.9% | ≥99.5% | ≥99.0% |
| **告警延迟** | ≤30秒 | ≤60秒 | ≤5分钟 |
| **数据采集延迟** | ≤1分钟 | ≤5分钟 | ≤15分钟 |
| **页面加载** | ≤2秒 | ≤5秒 | ≤10秒 |
| **API响应时间** | ≤500ms | ≤1s | ≤3s |
| **故障恢复时间** | ≤30分钟 | ≤2小时 | ≤24小时 |

#### 20.1.2 SLA 监控与报表

- **实时看板**：SLA 达成率实时展示
- **周报/月报**：自动生成 SLA 趋势报告
- **告警升级**：SLA 即将违约时自动升级

#### 20.1.3 数据库设计

```sql
-- SLA 配置表
CREATE TABLE sla_configs (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    level           VARCHAR(10) NOT NULL,  -- S1/S2/S3
    
    -- 可用性指标
    availability_target DECIMAL(5,4),     -- 0.9990
    
    -- 延迟指标
    alert_delay_max     INTEGER,          -- 秒
    collection_delay_max INTEGER,         -- 秒
    
    -- 性能指标
    page_load_max       INTEGER,          -- 毫秒
    api_response_max    INTEGER,          -- 毫秒
    
    -- 恢复指标
    recovery_time_max   INTEGER,          -- 分钟
    
    -- 业务关联
    asset_ids       UUID[],
    service_ids     UUID[],
    
    is_enabled      BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP,
    updated_at      TIMESTAMP
);

-- SLA 实际达成记录
CREATE TABLE sla_records (
    id              UUID PRIMARY KEY,
    sla_config_id   UUID REFERENCES sla_configs(id),
    record_date     DATE NOT NULL,
    
    -- 实际达成
    availability   DECIMAL(5,4),
    alert_delay     INTEGER,
    collection_delay INTEGER,
    page_load_avg   INTEGER,
    api_response_avg INTEGER,
    recovery_time   INTEGER,
    
    -- 计算
    uptime_seconds  INTEGER,              -- 可用秒数
    total_seconds   INTEGER,              -- 总秒数
    
    created_at      TIMESTAMP
);

CREATE INDEX idx_sla_records_config_date ON sla_records(sla_config_id, record_date);
```

---

### 20.2 备份与灾难恢复

#### 20.2.1 备份策略

| 数据类型 | 备份频率 | 保留期限 | 存储位置 | 方式 |
|----------|----------|----------|----------|------|
| **数据库全量** | 每天 02:00 | 30天 | 异地对象存储 | pg_dump |
| **数据库增量** | 每小时 | 7天 | 本地 + 异地 | WAL归档 |
| **配置文件** | 每次变更 | 90天 | 版本控制 SVN | - |
| **日志数据** | 实时 | 90天 | 时序数据库 | 滚动删除 |
| **用户上传** | 每天 | 30天 | 对象存储 | rsync |
| **系统配置** | 每周 | 12周 | 异地备份 | 镜像 |

#### 20.2.2 配置文件版本管理 (SVN)

> 使用机房本地 **SVN 服务器**进行配置文件版本管理。

##### SVN 仓库结构

```
svn://svn.internal.company.com/
├── configs/
│   ├── network/           # 网络设备配置
│   │   ├── switches/
│   │   └── firewalls/
│   ├── servers/          # 服务器配置
│   │   ├── linux/
│   │   └── windows/
│   └── applications/     # 应用配置
│       ├── nginx/
│       ├── mysql/
│       └── redis/
├── scripts/              # 运维脚本
│   ├── inspection/       # 巡检脚本
│   ├── automation/       # 自动化脚本
│   └── backup/           # 备份脚本
└── documents/            # 运维文档
    ├── procedures/       # 操作规程
    └── change_records/   # 变更记录
```

##### SVN 管理配置

```bash
# 检出配置仓库
svn checkout svn://svn.internal.company.com/configs /opt/configs

# 提交配置变更
cd /opt/configs/servers/linux
svn add server01.cfg
svn commit -m "更新 server01 网络配置"

# 查看变更历史
svn log server01.cfg
svn diff -r 100:HEAD server01.cfg
```

##### 与监控平台集成

```sql
-- 配置文件版本记录
CREATE TABLE config_versions (
    id              UUID PRIMARY KEY,
    
    -- 配置信息
    config_type     VARCHAR(20) NOT NULL,    -- network/server/application
    config_name     VARCHAR(100) NOT NULL,
    config_path     VARCHAR(500),
    
    -- SVN 信息
    svn_revision    VARCHAR(20),
    svn_author      VARCHAR(50),
    svn_commit_time TIMESTAMPTZ,
    commit_message  TEXT,
    
    -- 关联
    asset_id        UUID REFERENCES assets(id),
    
    created_at      TIMESTAMP DEFAULT NOW()
);
```

##### 定时同步

| 任务 | 说明 |
|------|------|
| 定时拉取 | 每小时从 SVN 同步最新配置 |
| 变更检测 | 检测配置变更，推送通知 |
| 版本记录 | 每次变更记录到数据库 |

> **注意**：SVN 服务器在机房本地，监控平台通过内网访问。

#### 20.2.3 灾难恢复方案

```
┌─────────────────────────────────────────────────────────────┐
│                     灾难恢复架构                               │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│   生产环境                                                    │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐   │
│   │  PostgreSQL │───▶│   WAL归档   │───▶│ 对象存储 S3 │   │
│   │   主库       │    │  (实时)     │    │  (异地)     │   │
│   └─────────────┘    └─────────────┘    └──────┬──────┘   │
│                                                 │            │
│                                                 ▼            │
│   灾备环境                         ┌─────────────────────┐   │
│   ┌─────────────┐    ┌─────────────┤   恢复流程           │   │
│   │  PostgreSQL │◀───│  恢复脚本   │   1. 下载最新备份   │   │
│   │   备库       │    │  (自动)     │   2. 执行 WAL 重放  │   │
│   └─────────────┘    └─────────────┘   3. 验证数据完整性  │   │
│                                                └─────────────┘│
└─────────────────────────────────────────────────────────────┘
```

#### 20.2.3 RTO / RPO 目标

| 灾难场景 | RTO (恢复时间目标) | RPO (恢复点目标) |
|----------|-------------------|------------------|
| **数据库故障** | ≤15分钟 | ≤5分钟 |
| **单节点故障** | ≤30分钟 | 0 (主从实时) |
| **机房级故障** | ≤2小时 | ≤1小时 |
| **全站故障** | ≤4小时 | ≤24小时 |

#### 20.2.4 数据库设计

```sql
-- 备份记录表
CREATE TABLE backup_records (
    id              UUID PRIMARY KEY,
    backup_type     VARCHAR(20) NOT NULL,  -- full/incremental/wal/config
    storage_path    VARCHAR(500),
    file_size       BIGINT,
    checksum        VARCHAR(64),
    
    -- 状态
    status          VARCHAR(20),           -- pending/running/completed/failed
    started_at      TIMESTAMP,
    completed_at    TIMESTAMP,
    error_message   TEXT,
    
    -- 保留
    expires_at      TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 灾难恢复演练记录
CREATE TABLE dr_drills (
    id              UUID PRIMARY KEY,
    drill_type      VARCHAR(50),           -- full_recovery/partial_recovery/failover
    plan_date       TIMESTAMP,
    executed_at     TIMESTAMP,
    
    -- 结果
    status          VARCHAR(20),           -- planned/running/success/failed
    rto_actual      INTEGER,               -- 实际恢复时间(分钟)
    rpo_actual      INTEGER,               -- 实际数据丢失(分钟)
    
    -- 问题
    issues          TEXT[],
    improvements    TEXT[],
    
    participants    VARCHAR(255)[],
    created_at      TIMESTAMP DEFAULT NOW()
);
```

---

### 20.3 值班与值守管理

#### 20.3.1 班次类型定义

本系统支持两种类型的运维工作模式：

| 类型 | 时间 | 周期 | 响应要求 |
|------|------|------|----------|
| **白班** | 09:00 - 17:30 | 工作日，节假日休息 | 15分钟内响应 |
| **夜班** | 17:30 - 次日 09:00 | 轮值，节假日不休息 | 30分钟内响应 |

##### 夜班轮换规则

```
┌─────────────────────────────────────────────────────────────┐
│                    夜班轮换周期 (4天循环)                     │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  第1天          第2天          第3天          第4天         │
│  ┌────────┐    ┌────────┐    ┌────────┐    ┌────────┐   │
│  │ 白班   │ →  │ 夜班   │ →  │ 休息   │ →  │ 休息   │   │
│  │ 09:00  │    │ 17:30  │    │        │    │        │   │
│  │   ↓    │    │   ↓    │    │        │    │        │   │
│  │ 17:30  │    │ 09:00  │    │        │    │        │   │
│  └────────┘    └────────┘    └────────┘    └────────┘   │
│                                                              │
│  循环继续...                                                 │
└─────────────────────────────────────────────────────────────┘
```

- **默认周期**：1个白班 → 1个夜班 → 休息2天 → 循环
- **可配置**：实际周期可根据团队情况调整

##### 白班加班规则

- 节假日加班：按加班时长记录，可安排调休
- 临时加班：需审批，加班费/调休由HR系统对接

#### 20.3.2 请假与换休

当运维人员请假或换休时，排班需要自动调整：

##### 请假类型

| 类型 | 说明 | 是否需要替班 |
|------|------|-------------|
| **事假** | 个人事务请假 | 是 |
| **病假** | 生病请假 | 是 |
| **年假** | 带薪年假 | 是 |
| **换休** | 加班换调休 | 是 |
| **婚假/产假等** | 法定假期 | 是 |

##### 请假流程

```
提交请假申请
     │
     ▼
审批 (主管/经理)
     │
   ┌─┴─┐
 通过  拒绝
     │
     ▼
系统自动调整排班
     │
     ├──▶ 原时段标记为"空缺"
     │
     ├──▶ 替班人员自动补位
     │
     └──▶ 工单重新指派
```

#### 20.3.3 替班管理

##### 替班场景

| 场景 | 处理方式 |
|------|----------|
| **临时请假** | 主管指派替班人员 |
| **替班承诺** | 同事间协商换班 |
| **主管调配** | 紧急情况下主管直接安排 |

##### 替班规则

- 替班需提前 **24小时** 申请（紧急情况除外）
- 替班后原人员仍保留该时段查看权限
- 替班工时不计入考核（记录在案）
- 替班不可转让（不可替班后再转给第三人）

#### 20.3.4 排班联动

##### 与工单指派联动

```
工单创建 → 智能派单
              │
              ├──▶ 检查当前值班人员
              │
              ├──▶ 检查白班/夜班状态
              │
              ├──▶ 检查请假/替班情况
              │
              └──▶ 指派给当前可接单人
                  
指派优先级：
1. 当前值班人员（夜班/白班）
2. 替班人员
3. 白班默认人员
4. 主管兜底
```

##### 与告警联动

```
告警触发 → 告警升级
              │
              ├──▶ 查找当前值班人员
              │
              ├──▶ 检查在线状态
              │
              ├──▶ 未响应 → 升级主管
              │
              └──▶ 记录响应日志
```

#### 20.3.5 告警升级策略

```
告警产生
    │
    ▼
级别: Warning ──▶ 值班工程师 ──▶ 30分钟未响应 ──▶ 升级
    │
级别: Critical ──▶ 值班工程师 ──▶ 15分钟未响应 ──▶ 升级
    │
级别: Emergency ──▶ 值班主管 ──▶ 立即通知
```

#### 20.3.6 排班数据库设计

```sql
-- 班次类型配置
CREATE TABLE shift_types (
    id              UUID PRIMARY KEY,
    code            VARCHAR(20) NOT NULL UNIQUE,  -- day_shift/night_shift
    name            VARCHAR(50) NOT NULL,           -- 白班/夜班
    start_time      TIME NOT NULL,                 -- 09:00 / 17:30
    end_time        TIME NOT NULL,                 -- 17:30 / 09:00
    is_night        BOOLEAN DEFAULT FALSE,         -- 是否跨天
    
    -- 工作日规则
    work_days       INTEGER[],                     -- [1,2,3,4,5] 工作日
    is_holiday      BOOLEAN DEFAULT FALSE,         -- 节假日是否上班
    
    -- 响应要求
    response_timeout INTEGER DEFAULT 900,          -- 响应超时(秒)
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 初始化班次类型
INSERT INTO shift_types (code, name, start_time, end_time, is_night, work_days, response_timeout) VALUES
('day_shift', '白班', '09:00', '17:30', FALSE, ARRAY[1,2,3,4,5], 900),       -- 15分钟
('night_shift', '夜班', '17:30', '09:00', TRUE, ARRAY[1,2,3,4,5,6,7], 1800); -- 30分钟

-- 排班周期配置
CREATE TABLE shift_cycles (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    description     TEXT,
    
    -- 周期配置
    cycle_days      INTEGER NOT NULL DEFAULT 4,    -- 4天循环
    shifts_in_cycle JSONB NOT NULL,                -- ['day_shift', 'night_shift', 'off', 'off']
    
    -- 有效性
    is_active       BOOLEAN DEFAULT TRUE,
    valid_from      DATE NOT NULL,
    valid_to        DATE,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 人员排班表
CREATE TABLE user_schedules (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id),
    shift_type_id   UUID NOT NULL REFERENCES shift_types(id),
    cycle_id        UUID REFERENCES shift_cycles(id),
    
    -- 排班日期
    schedule_date   DATE NOT NULL,
    
    -- 班次时间（实际）
    shift_start     TIMESTAMPTZ NOT NULL,
    shift_end       TIMESTAMPTZ NOT NULL,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'scheduled',  -- scheduled/vacant/covered/cancelled
    
    -- 替班信息
    original_user_id UUID REFERENCES users(id),    -- 原值班人
    cover_user_id   UUID REFERENCES users(id),     -- 替班人
    cover_reason    VARCHAR(50),                    -- 替班原因
    
    -- 加班信息
    is_overtime     BOOLEAN DEFAULT FALSE,
    overtime_hours  DECIMAL(4,2),
    overtime_reason TEXT,
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP,
    
    UNIQUE(user_id, schedule_date)
);

CREATE INDEX idx_user_schedules_date ON user_schedules(schedule_date);
CREATE INDEX idx_user_schedules_user_date ON user_schedules(user_id, schedule_date);

-- 请假申请表
CREATE TABLE leave_requests (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id),
    
    -- 请假信息
    leave_type      VARCHAR(20) NOT NULL,          -- sick/annual/personal/marriage/maternity
    start_date      DATE NOT NULL,
    end_date        DATE NOT NULL,
    total_days      DECIMAL(3,1) NOT NULL,
    
    -- 原因
    reason          TEXT,
    
    -- 审批
    status          VARCHAR(20) DEFAULT 'pending',  -- pending/approved/rejected
    approver_id     UUID REFERENCES users(id),
    approved_at     TIMESTAMP,
    rejection_reason TEXT,
    
    -- 替班安排
    need_cover      BOOLEAN DEFAULT TRUE,
    cover_user_id   UUID REFERENCES users(id),
    
    -- 关联排班
    schedule_ids    UUID[],
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP
);

-- 替班申请表
CREATE TABLE shift_cover_requests (
    id              UUID PRIMARY KEY,
    requester_id    UUID NOT NULL REFERENCES users(id),  -- 申请人
    
    -- 原时段
    original_date   DATE NOT NULL,
    original_shift  VARCHAR(20),
    
    -- 目标时段
    target_date     DATE NOT NULL,
    target_shift    VARCHAR(20),
    
    -- 原因
    reason          TEXT,
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'pending',   -- pending/approved/rejected/cancelled
    
    -- 审批
    approver_id     UUID REFERENCES users(id),
    approved_at     TIMESTAMP,
    
    -- 替班人（审批后填入）
    cover_user_id   UUID REFERENCES users(id),
    
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP
);

-- 值班日志
CREATE TABLE oncall_logs (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id),
    schedule_id     UUID REFERENCES user_schedules(id),
    alert_id        UUID REFERENCES alerts(id),
    
    -- 时间
    alert_time      TIMESTAMPTZ,
    first_ack_time  TIMESTAMPTZ,
    resolved_time   TIMESTAMPTZ,
    
    -- 时长(秒)
    time_to_ack     INTEGER,
    time_to_resolve INTEGER,
    
    -- 状态
    status          VARCHAR(20),  -- acknowledged/resolved/escalated
    
    notes           TEXT,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 值班排班视图（查询当前值班人员）
CREATE VIEW v_current_oncall AS
SELECT 
    us.schedule_date,
    us.shift_type_id,
    st.name AS shift_name,
    us.user_id,
    u.username,
    u.nickname,
    u.phone,
    CASE 
        WHEN us.status = 'covered' THEN (SELECT nickname FROM users WHERE id = us.cover_user_id)
        ELSE NULL 
    END AS cover_by
FROM user_schedules us
JOIN shift_types st ON st.id = us.shift_type_id
JOIN users u ON u.id = COALESCE(us.cover_user_id, us.user_id)
WHERE us.schedule_date = CURRENT_DATE
AND us.status IN ('scheduled', 'covered');
```

---

*排班系统已支持白班/夜班循环、请假联动、替班管理、工单自动指派 - v1.14*

### 20.4 维护窗口管理

#### 20.4.1 维护窗口类型

| 类型 | 说明 | 时长 | 审批 |
|------|------|------|------|
| **计划维护** | 系统升级、硬件更换 | 4小时内 | 需要 |
| **紧急维护** | 故障修复 | 2小时内 | 事后补批 |
| **日常巡检** | 例行检查 | 1小时内 | 无需 |

#### 20.4.2 维护窗口功能

- **窗口预约**：提前申请维护时间段
- **告警静默**：维护期间相关告警自动静默
- **通知推送**：维护开始/结束前自动通知
- **变更追踪**：记录维护操作历史

#### 20.4.3 数据库设计

```sql
-- 维护窗口表
CREATE TABLE maintenance_windows (
    id              UUID PRIMARY KEY,
    title           VARCHAR(200) NOT NULL,
    description     TEXT,
    
    -- 时间窗口
    planned_start   TIMESTAMPTZ NOT NULL,
    planned_end     TIMESTAMPTZ NOT NULL,
    actual_start    TIMESTAMPTZ,
    actual_end      TIMESTAMPTZ,
    
    -- 类型
    window_type     VARCHAR(20) NOT NULL,   -- planned/emergency/daily
    
    -- 范围（影响的资产/服务）
    target_type     VARCHAR(20),            -- asset/service/global
    target_ids      UUID[],
    
    -- 审批
    status          VARCHAR(20),           -- pending/approved/rejected/completed
    approver_id     UUID REFERENCES users(id),
    approved_at     TIMESTAMP,
    rejection_reason TEXT,
    
    -- 执行人
    operator_id     UUID REFERENCES users(id),
    
    -- 告警静默
    silence_alerts  BOOLEAN DEFAULT TRUE,
    
    created_by      UUID REFERENCES users(id),
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP
);
```

#### 20.4.4 计划离线与告警抑制

在专线维护、设备维护等场景中，设备或链路会短暂离线。这是**预期内的离线**，不应触发告警。

##### 告警抑制逻辑

```
设备/专线离线
     │
     ▼
检查是否存在有效的"计划离线"记录
     │
   ┌─┴─┐
   有   无
   │   │
   ▼   ▼
检查离线时长    正常告警
是否超过预期
   │
 ┌─┴─┐
 是   否
 │   │
 ▼   ▼
触发告警   不告警（计划内）
```

##### 工单中的预期离线时长

在创建维护工单时，需填写**预期离线时长**：

```
工单创建页面：

┌─────────────────────────────────────────────────────────────────┐
│  创建维护工单                                                   │
├─────────────────────────────────────────────────────────────────┤
│  工单类型: [维护 ▼]                                             │
│  标题: 核心交换机维护                                            │
│  ─────────────────────────────────────────────────────────────  │
│  目标设备/专线:                                                 │
│  ☑ 核心交换机-01 (192.168.1.1)                                 │
│  ─────────────────────────────────────────────────────────────  │
│  预期离线时长: [  1  ] 小时 ▼                                  │
│  (5分钟 / 15分钟 / 30分钟 / 1小时 / 2小时 / 4小时 / 自定义)     │
│  ─────────────────────────────────────────────────────────────  │
│  维护内容: 固件升级                                              │
│  计划开始时间: [2024-02-15 02:00]                             │
└─────────────────────────────────────────────────────────────────┘
```

##### 数据库设计增强

```sql
-- 计划离线记录表
CREATE TABLE planned_outages (
    id              UUID PRIMARY KEY,
    
    -- 关联工单
    ticket_id       UUID REFERENCES tickets(id),
    
    -- 离线对象
    target_type     VARCHAR(20) NOT NULL,   -- asset/line
    target_id       UUID NOT NULL,
    
    -- 预期离线时间
    expected_start  TIMESTAMPTZ NOT NULL,
    expected_end    TIMESTAMPTZ NOT NULL,   -- 根据预期时长计算
    expected_duration INTEGER NOT NULL,    -- 预期离线时长(秒)
    
    -- 实际离线时间（由采集器自动记录）
    actual_start    TIMESTAMPTZ,
    actual_end      TIMESTAMPTZ,
    
    -- 告警判断
    allow_exceed    BOOLEAN DEFAULT FALSE,  -- 是否允许超时长
    exceed_tolerance INTEGER DEFAULT 300,  -- 允许超出时长(秒)
    
    -- 状态
    status          VARCHAR(20) DEFAULT 'pending',  -- pending/active/completed/cancelled
    
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_planned_outages_target ON planned_outages(target_type, target_id, expected_start, expected_end);
CREATE INDEX idx_planned_outages_ticket ON planned_outages(ticket_id);

-- 告警判断逻辑
CREATE OR REPLACE FUNCTION should_suppress_alert(
    p_target_type VARCHAR,
    p_target_id UUID,
    p_occurred_at TIMESTAMPTZ
) RETURNS BOOLEAN AS $$
DECLARE
    v_result BOOLEAN := FALSE;
    v_outage RECORD;
BEGIN
    -- 查找当前时间是否在计划离线范围内
    SELECT * INTO v_outage
    FROM planned_outages
    WHERE target_type = p_target_type
      AND target_id = p_target_id
      AND status IN ('pending', 'active')
      AND expected_start <= p_occurred_at
      AND expected_end >= p_occurred_at;
    
    IF FOUND THEN
        v_result := TRUE;  -- 在计划内，不告警
    END IF;
    
    RETURN v_result;
END;
$$ LANGUAGE plpgsql;

-- 告警规则增强：关联计划离线
ALTER TABLE alert_rules ADD COLUMN check_planned_outage BOOLEAN DEFAULT TRUE;
```

##### 告警引擎处理流程

```
告警引擎触发告警前：

1. 获取告警事件 (设备/专线离线)
     │
     ▼
2. 调用 should_suppress_alert()
     │
   ┌─┴─┐
 返回TRUE  返回FALSE
   │   │
   ▼   ▼
 不生成告警  继续判断
   │   │
   │   ▼
   │ 3. 检查是否超出预期时长
   │    (actual_end - actual_start > expected_duration)
   │    │
   │  ┌─┴─┐
   │  是   否
   │  │   │
   │  ▼   ▼
   │ 超时告警  不告警
   │
   └─────┘
     │
     ▼
4. 生成告警 / 发送通知
```

##### 告警展示区别

在告警列表中，区分**计划内**和**计划外**离线：

```
告警列表：

┌─────────────────────────────────────────────────────────────────┐
│  全部  未处理  已处理  计划内(不告警)                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  🔴 核心交换机-01 已离线 2小时  [已超时]  计划维护工单 #123    │
│  🟡 服务器-03 响应延迟高     [处理中]  阈值触发               │
│  ⚪ 专线-ZJ002 已离线 30分钟 [计划内]  维护工单 #124          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘

- 🔴 红色：计划外告警，需要处理
- 🟡 黄色：正常阈值告警
- ⚪ 灰色：计划内离线，不告警（仅记录）
```

---

### 20.5 容量规划与监控

#### 20.5.1 容量监控指标

| 资源 | 预警阈值 | 告警阈值 | 严重阈值 |
|------|----------|----------|----------|
| **CPU 使用率** | ≥70% | ≥85% | ≥95% |
| **内存使用率** | ≥70% | ≥85% | ≥95% |
| **磁盘使用率** | ≥70% | ≥80% | ≥90% |
| **网络带宽** | ≥70% | ≥85% | ≥95% |
| **数据库连接** | ≥70% | ≥85% | ≥95% |
| **时序数据点** | - | 日增长量超阈值 | - |

#### 20.5.2 容量趋势分析

- **周趋势**：本周 vs 上周资源使用对比
- **月趋势**：月度增长曲线预测
- **季度报告**：容量规划建议

#### 20.5.3 扩容触发规则

```yaml
# 扩容规则示例
auto_scaling:
  rules:
    - name: "CPU 持续高负载"
      condition: "cpu_avg > 80% for 30 minutes"
      action: "scale_out"
      
    - name: "磁盘空间不足"
      condition: "disk_free < 20%"
      action: "alert_only"  # 需人工介入
      
    - name: "数据库连接池耗尽"
      condition: "db_connections > 90% max"
      action: "scale_out + alert"
```

#### 20.5.4 数据库设计

```sql
-- 容量阈值配置
CREATE TABLE capacity_thresholds (
    id              UUID PRIMARY KEY,
    resource_type   VARCHAR(50) NOT NULL,   -- cpu/memory/disk/network/db
    metric_name     VARCHAR(100),
    
    -- 阈值
    warning_level   DECIMAL(5,2),            -- 百分比
    critical_level  DECIMAL(5,2),
    
    -- 持续时间
    duration_seconds INTEGER DEFAULT 300,    # 5分钟
    
    -- 动作
    action          VARCHAR(50),             # alert/scale/auto_remediate
    
    is_enabled      BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP
);

-- 容量趋势记录
CREATE TABLE capacity_trends (
    id              UUID PRIMARY KEY,
    record_date     DATE NOT NULL,
    resource_type   VARCHAR(50) NOT NULL,
    
    -- 快照值
    total_capacity  DECIMAL(15,2),
    used_capacity   DECIMAL(15,2),
    utilization     DECIMAL(5,2),
    
    -- 趋势
    daily_growth    DECIMAL(15,2),
    days_until_full INTEGER,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_capacity_trends_date ON capacity_trends(record_date, resource_type);
```

---

### 20.6 IP 地址管理 (IPAM)

> 作为运维团队，IP 地址是稀缺资源，需要精细化管理。从网络专家角度，IPAM 是网络基础设施的核心组件。

#### 20.6.1 IP 地址池管理

```sql
-- IP地址池表
CREATE TABLE ip_pools (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    network         CIDR NOT NULL,
    vlan_id         INTEGER,
    gateway         INET,
    netmask         INET,
    allocation_type VARCHAR(20),
    start_ip        INET,
    end_ip          INET,
    reserved_start  INET,
    reserved_end    INET,
    description     TEXT,
    idc_id          UUID REFERENCES idc(id),
    rack_id         UUID REFERENCES racks(id),
    created_at      TIMESTAMP DEFAULT NOW()
);

-- IP地址分配记录
CREATE TABLE ip_allocations (
    id              UUID PRIMARY KEY,
    pool_id         UUID REFERENCES ip_pools(id),
    ip_address      INET NOT NULL,
    allocation_type VARCHAR(20),
    assigned_to     VARCHAR(255),
    asset_id        UUID REFERENCES assets(id),
    purpose         VARCHAR(50),
    status          VARCHAR(20) DEFAULT 'allocated',
    allocated_at    TIMESTAMP DEFAULT NOW(),
    released_at     TIMESTAMP,
    created_by      UUID REFERENCES users(id)
);
```

#### 20.6.2 IP 冲突检测

```sql
-- IP冲突记录
CREATE TABLE ip_conflicts (
    id              UUID PRIMARY KEY,
    ip_address      INET NOT NULL,
    mac_address_1   VARCHAR(17),
    hostname_1      VARCHAR(255),
    mac_address_2   VARCHAR(17),
    hostname_2      VARCHAR(255),
    detected_at     TIMESTAMP DEFAULT NOW(),
    resolved_at     TIMESTAMP,
    resolution      TEXT
);
```

---

### 20.7 DNS 管理

#### 20.7.1 DNS 区域与记录

```sql
-- DNS 区域表
CREATE TABLE dns_zones (
    id              UUID PRIMARY KEY,
    zone_name       VARCHAR(100) NOT NULL UNIQUE,
    zone_type       VARCHAR(20),
    primary_server  VARCHAR(255),
    admin_email     VARCHAR(255),
    ttl             INTEGER DEFAULT 3600,
    is_active       BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- DNS 记录表
CREATE TABLE dns_records (
    id              UUID PRIMARY KEY,
    zone_id         UUID REFERENCES dns_zones(id),
    record_type     VARCHAR(10) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    value           VARCHAR(500) NOT NULL,
    ttl             INTEGER,
    priority        INTEGER,
    asset_id        UUID REFERENCES assets(id),
    status          VARCHAR(20) DEFAULT 'active',
    change_ticket_id UUID REFERENCES tickets(id),
    created_at      TIMESTAMP DEFAULT NOW()
);
```

---

### 20.8 网络变更管理

> 基于实际运维变更流程，参考标准变更申请表格式。

#### 20.8.1 变更流程

```
┌─────────────────────────────────────────────────────────────────┐
│                    运维变更流程                                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  变更申请 ──▶ 风险评估 ──▶ 审批 ──▶ 执行 ──▶ 结果确认      │
│      │            │            │        │           │               │
│      ▼            ▼            ▼        ▼           ▼               │
│  申请人填写   负责人评估   经理审批  执行计划   上线确认          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

#### 20.8.2 变更表单

##### 1. 变更申请表（申请人填写）

| 字段 | 说明 |
|------|------|
| 变更系统名称 | 要变更的系统/设备 |
| 申请日期 | 申请时间 |
| 计划执行日期 | 预计变更时间 |
| 变更申请人 | 申请人姓名 |
| 联系方式 | 电话/邮箱 |
| 变更申请人单位/部门 | 所属部门 |
| 变更需求内容 | 详细变更需求描述 |

##### 2. 审批意见（项目经理审批）

| 字段 | 说明 |
|------|------|
| 审批意见 | 同意/不同意 |
| 审批人签字 | |
| 审批日期 | |

##### 3. 风险评估（变更负责人填写）

| 字段 | 说明 |
|------|------|
| 变更系统名称 | |
| 变更类别 | 类型 |
| 变更期间的影响 | 对业务的影响 |
| 影响的系统 | 受影响的系统列表 |
| 影响的时间 | 变更窗口时间 |
| 变更失败的影响 | 失败后的后果 |
| 业务风险评估 | |
| 技术风险评估 | |

##### 4. 执行计划

| 字段 | 说明 |
|------|------|
| 变更执行计划内容 | 详细执行步骤 |
| 开发/实施单位及人名 | |
| 变更负责人及人名 | |
| 日期 | |
| 变更测试结果 | |
| 测试单位及人名 | |
| 变更回退方案 | 回退步骤和前提条件 |

##### 5. 审批意见（部门经理）

| 字段 | 说明 |
|------|------|
| 部门经理审批 | 同意/不同意 |
| 签字 | |
| 日期 | |

##### 6. 变更结果确认

| 字段 | 说明 |
|------|------|
| 上线操作日期 | |
| 上线操作人 | |
| 变更是否成功 | 是/否 |
| 变更负责人 | |
| 不成功的变更是否导致问题 | 是/否 |
| 实际执行时间 | 开始 ~ 结束 |
| 变更申请人确认 | 确认变更结果 |
| 确认日期时间 | |

> **注**：变更流程建议两个工作日内走完，如领导不在需先通过其他方式确认，事后补签。

#### 20.8.3 数据库设计

```sql
-- 变更申请表
CREATE TABLE change_applications (
    id              UUID PRIMARY KEY,
    ticket_id       UUID REFERENCES tickets(id),
    
    -- 申请人信息
    applicant_name VARCHAR(50) NOT NULL,
    applicant_phone VARCHAR(20),
    applicant_email VARCHAR(100),
    applicant_dept VARCHAR(100),
    apply_date     DATE,
    planned_date   DATE,
    
    -- 变更内容
    system_name    VARCHAR(100) NOT NULL,
    change_type   VARCHAR(50),
    change_content TEXT NOT NULL,
    
    -- 审批（项目经理）
    pm_approval    VARCHAR(20),             -- approved/rejected
    pm_approver   VARCHAR(50),
    pm_approved_at TIMESTAMP,
    pm_comment    TEXT,
    
    -- 风险评估（变更负责人）
    risk_assessor VARCHAR(50),
    impact_systems TEXT,
    impact_time   VARCHAR(100),
    failure_impact TEXT,
    business_risk TEXT,
    technical_risk TEXT,
    
    -- 执行计划
    execution_plan TEXT,
    executor_unit  VARCHAR(100),
    executor      VARCHAR(50),
    executor_date DATE,
    
    test_result   TEXT,
    tester_unit   VARCHAR(100),
    tester       VARCHAR(50),
    rollback_plan TEXT,
    
    -- 审批（部门经理）
    mgr_approval VARCHAR(20),             -- approved/rejected
    mgr_approver VARCHAR(50),
    mgr_approved_at TIMESTAMP,
    mgr_comment  TEXT,
    
    -- 结果确认
    execution_start TIMESTAMP,
    execution_end   TIMESTAMP,
    is_success    BOOLEAN,
    success_comment TEXT,
    applicant_confirm BOOLEAN,
    confirm_date  TIMESTAMP,
    
    status        VARCHAR(20) DEFAULT 'draft',  -- draft/pending_pm/pending_mgr/approved/executing/completed/cancelled
    
    created_at    TIMESTAMP DEFAULT NOW(),
    updated_at    TIMESTAMP
);
```

#### 20.8.4 变更流程状态机

```
draft → pending_pm → pending_mgr → approved → executing → completed
  ↓         ↓              ↓            ↓           ↑
  └─────────┴──────────────┴─────────────┴───────────┘ (rejected/cancelled)
```

---

### 20.9 日常巡检系统

> **优化方案**：利用**堡垒机的批量定时任务功能**执行巡检，避免监控平台直接登录设备，提升安全性。

#### 20.9.1 巡检架构（堡垒机方案）

```
┌─────────────────────────────────────────────────────────────────┐
│              堡垒机执行巡检架构 (推荐)                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   监控平台                                                     │
│   ┌─────────────┐                                              │
│   │  巡检调度  │ ───── 发起巡检任务 ──▶ 堡垒机               │
│   │  (触发)    │          API 调用                           │
│   └─────────────┘                                              │
│          ▲                                                     │
│          │ 结果上报                                             │
│          │                                                     │
│   ┌─────┴─────────┐                                           │
│   │   告警/展示   │ ◀────────────────────────────────────    │
│   └───────────────┘                                           │
│                                                                  │
│   堡垒机                                                        │
│   ┌─────────────────────────────────────────────────────┐       │
│   │  批量定时任务                                          │       │
│   │  ├── 任务A: 时间同步检查 (所有服务器)                 │       │
│   │  ├── 任务B: 安全基线检查 (所有服务器)                │       │
│   │  └── 任务C: 自定义巡检任务                           │       │
│   └─────────────────────────────────────────────────────┘       │
│          │                                                     │
│          ▼                                                     │
│   ┌─────────────────────────────────────────────────────┐       │
│   │  执行结果                                              │       │
│   │  ├── 输出日志 ──────────▶ Splunk/日志服务器         │       │
│   │  └── 结果文件 ──────────▶ 监控平台 API             │       │
│   └─────────────────────────────────────────────────────┘       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**安全优势**：

| 对比项 | 直接 SSH 登录 | 堡垒机方案 |
|--------|---------------|------------|
| **认证** | 监控平台存储密钥 | 堡垒机统一管理 |
| **权限** | 需要SSH到所有设备 | 堡垒机授权即可 |
| **审计** | 分散记录 | 堡垒机统一审计 |
| **变更** | 增加设备需改配置 | 堡垒机后台加设备 |

#### 20.9.2 巡检脚本管理

> 巡检脚本由运维团队统一编写，兼容各版本 Linux，堡垒机管理员负责部署到堡垒机。

##### 脚本开发与管理流程

```
┌─────────────────────────────────────────────────────────────────┐
│                  巡检脚本管理流程                                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. 脚本开发                                                   │
│     运维团队编写兼容脚本                                         │
│     (CentOS/Ubuntu/Debian/SUSE 等)                             │
│         │                                                        │
│         ▼                                                        │
│  2. 脚本测试                                                   │
│     在测试环境验证兼容性                                         │
│         │                                                        │
│         ▼                                                        │
│  3. 脚本入库                                                   │
│     提交到脚本仓库 (SVN)                                        │
│         │                                                        │
│         ▼                                                        │
│  4. 堡垒机部署                                                 │
│     堡垒机管理员拉取脚本                                          │
│     配置到堡垒机批量任务                                         │
│         │                                                        │
│         ▼                                                        │
│  5. 任务执行                                                   │
│     监控平台通过 API 触发执行                                    │
│     堡垒机执行批量任务                                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

##### 脚本开发规范

```bash
#!/bin/bash
#==============================================================================
#  Linux 巡检脚本 (兼容多版本)
#  适用: CentOS 7+/Ubuntu 18.04+/Debian 10+/SUSE 15+
#  作者: 运维团队
#  版本: 1.0.0
#==============================================================================

# 检测操作系统
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        case "$ID" in
            centos|rhel|rocky|alma) OS="rhel" ;;
            ubuntu|debian) OS="debian" ;;
            sles|opensuse) OS="sles" ;;
            *) OS="unknown" ;;
        esac
    elif [ -f /etc/redhat-release ]; then
        OS="rhel"
    else
        OS="unknown"
    fi
}

# 检测命令是否存在 (兼容多版本)
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# 安全基线检查 - 兼容版
check_security_baseline() {
    RESULTS=()
    
    # 1. 检查 root 登录 (兼容不同 SSH 配置写法)
    if grep -qE "^\s*PermitRootLogin\s+no" /etc/ssh/sshd_config 2>/dev/null; then
        RESULTS+=("OK:root登录已禁用")
    else
        RESULTS+=("WARN:root登录未禁用")
    fi
    
    # 2. 检查空密码账号 (兼容不同系统)
    if [ "$OS" = "rhel" ]; then
        EMPTY=$(awk -F: '($2==""){print}' /etc/shadow 2>/dev/null)
    else
        EMPTY=$(sudo awk -F: '($2==""){print}' /etc/shadow 2>/dev/null)
    fi
    [ -z "$EMPTY" ] && RESULTS+=("OK:无空密码") || RESULTS+=("WARN:有空密码:$EMPTY")
    
    # 3. 检查 UID 0 (兼容)
    UID0=$(awk -F: '($3==0 && $1!="root"){print}' /etc/passwd 2>/dev/null)
    [ -z "$UID0" ] && RESULTS+=("OK:无多余UID0") || RESULTS+=("WARN:多余UID0:$UID0")
    
    # 4. 检查开放端口 (兼容 netstat/ss)
    if command_exists ss; then
        PORTS=$(ss -tuln 2>/dev/null | grep LISTEN | awk '{print $5}' | awk -F: '{print $NF}' | sort -n | uniq)
    else
        PORTS=$(netstat -tuln 2>/dev/null | grep LISTEN | awk '{print $4}' | awk -F: '{print $NF}' | sort -n | uniq)
    fi
    RESULTS+=("INFO:开放端口:$PORTS")
    
    # 输出 JSON 格式
    echo "{"
    echo "  \"hostname\": \"$HOSTNAME\","
    echo "  \"os\": \"$OS\","
    echo "  \"checks\": ["
    local first=true
    for r in "${RESULTS[@]}"; do
        [ "$first" = false ] && echo "," || first=false
        echo -n "    \"$r\""
    done
    echo ""
    echo "  ]"
    echo "}"
}

# 主程序
detect_os
check_security_baseline
```

##### 脚本版本管理

```bash
# 脚本仓库结构
inspection-scripts/
├── README.md
├── LICENSE
├── security/
│   ├── baseline.sh          # 安全基线检查 v1.0
│   ├── baseline_v1.1.sh
│   └── tripwire_parser.sh   # Tripwire 报告解析
├── time/
│   ├── ntp_check.sh         # NTP 检查
│   └── timezone_check.sh    # 时区检查
├── resource/
│   ├── cpu_check.sh         # CPU 检查
│   ├── memory_check.sh      # 内存检查
│   └── disk_check.sh        # 磁盘检查
└── compat/                  # 兼容性工具库
    ├── os_detect.sh
    └── command_exists.sh
```

##### 堡垒机管理员职责

| 工作 | 说明 |
|------|------|
| **拉取脚本** | 从 SVN 仓库拉取最新脚本 |
| **配置任务** | 在堡垒机配置批量任务 |
| **目标主机** | 将目标服务器加入任务 |
| **执行调度** | 设置执行时间和频率 |
| **结果查看** | 查看执行日志和结果 |

##### 监控平台职责

| 工作 | 说明 |
|------|------|
| **任务触发** | 通过 API 触发堡垒机任务 |
| **结果收集** | 接收/拉取执行结果 |
| **分析展示** | 展示巡检结果 |
| **告警** | 异常结果生成告警 |

> **注意**：监控平台**不直接管理脚本**，只负责触发和结果收集。脚本由运维团队开发和堡垒机管理员部署。

##### 任务类型

| 任务类型 | 说明 | 执行频率 |
|----------|------|----------|
| **时间同步检查** | 检查系统时间、NTP状态 | 每小时 |
| **安全基线检查** | root登录、端口、权限等 | 每日 |
| **资源使用检查** | CPU/内存/磁盘 | 每日 |
| **自定义检查** | 业务自定义脚本 | 按需 |

##### 堡垒机任务模板

```
堡垒机批量任务配置示例：

任务名称: 每日安全基线巡检
────────────────────────────────
执行方式: 批量并行
目标主机: 资产列表 (按标签筛选)
用户: monitor (普通账号)

执行脚本:
#!/bin/bash
# 安全基线检查脚本

# 1. 检查 root 登录
grep "^PermitRootLogin" /etc/ssh/sshd_config

# 2. 检查空密码
awk -F: '($2==""){print}' /etc/shadow

# 3. 检查 UID 0 账号
awk -F: '($3==0 && $1!="root"){print}' /etc/passwd

# 4. 检查开放端口
netstat -tuln

# 5. 检查 SSH 服务
systemctl is-active sshd

输出:
- 标准输出 → 日志服务器
- 返回码 → 监控平台

执行频率: 每日 03:00
```

#### 20.9.3 巡检结果收集

##### 方式一：日志上报（推荐）

```
堡垒机执行 → 输出到 Syslog → Splunk → 监控平台解析
```

```bash
# 巡检脚本输出到 Syslog
logger -t "INSPECTION" -p info "hostname=$HOSTNAME check=security result=OK"
```

##### 方式二：API 上报

```
堡垒机执行 → 调用监控平台 API 上报结果
```

```bash
# 结果上报脚本
curl -X POST https://monitor.example.com/api/v1/inspection/results \
    -H "Authorization: Bearer $TOKEN" \
    -d "{
        \"hostname\": \"$HOSTNAME\",
        \"check_type\": \"security\",
        \"results\": [...],
        \"timestamp\": \"$(date -Iseconds)\"
    }"
```

#### 20.9.4 监控平台巡检管理

```sql
-- 巡检任务配置（关联堡垒机）
CREATE TABLE inspection_tasks (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    
    -- 堡垒机关联
    bastion_id      UUID REFERENCES bastion_hosts(id),
    
    -- 巡检配置
    check_type      VARCHAR(20) NOT NULL,    -- time/security/resource
    target_filter   JSONB,                   -- 目标筛选条件
    script_content  TEXT,                    -- 巡检脚本内容
    
    -- 执行配置
    schedule_type   VARCHAR(20),             -- cron/manual
    schedule_cron  VARCHAR(50),
    
    -- 告警阈值
    alert_threshold JSONB,                   -- 告警配置
    
    is_enabled      BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 巡检结果
CREATE TABLE inspection_results (
    id              UUID PRIMARY KEY,
    task_id         UUID REFERENCES inspection_tasks(id),
    hostname        VARCHAR(100) NOT NULL,
    asset_id        UUID REFERENCES assets(id),
    
    check_time      TIMESTAMPTZ NOT NULL,
    check_type      VARCHAR(20),
    
    -- 结果
    result          VARCHAR(20),              -- ok/warning/critical/error
    details         JSONB,
    
    -- 告警状态
    alerted         BOOLEAN DEFAULT FALSE,
    
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_inspection_results_host ON inspection_results(hostname, check_time);
```

#### 20.9.5 巡检项说明

##### 时间同步检查（分层）

> 机房时间同步采用**分层架构**，巡检也分两层进行：

```
┌─────────────────────────────────────────────────────────────────┐
│                    时间同步分层架构                                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  层级1: NTP服务器层级                                              │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  互联网 NTP 服务器                                          │   │
│  │  (time.google.com, time.cloudflare.com, ntp.aliyun.com)│   │
│  │                      ↑                                     │   │
│  │  同步检查 ─────────────────────────────────────────────│   │
│  │                      ↓                                     │   │
│  │  机房内部 NTP 服务器 (NTP-01)                           │   │
│  │  (192.168.100.10)                                        │   │
│  └─────────────────────────────────────────────────────────┘   │
│                           ↑                                      │
│  同步检查 ────────────────────────────────────────────────────    │
│                           ↓                                      │
│  层级2: 普通设备                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  服务器、网络设备、虚拟机                                  │   │
│  │  ↓                                                      │   │
│  │  与机房 NTP 服务器同步                                    │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

###### 检查类型一：机房 NTP 服务器与互联网时钟同步

| 检查项 | 说明 | 检查方法 |
|--------|------|----------|
| **互联网连通性** | NTP 服务器能否访问互联网 | `ping time.google.com` |
| **上游同步状态** | NTP 服务器是否与上游同步 | `ntpdc -p` 或 `chronyc sources` |
| **时间偏差** | 与互联网时间偏差 | `ntpdate -q time.google.com` |
| **层级 Stratum** | NTP Stratum 级别 | 应为 Stratum 2 或 3 |

**阈值**：

| 偏差范围 | 判定 | 动作 |
|----------|------|------|
| < 100ms | 正常 | - |
| 100ms - 1秒 | 警告 | 记录 |
| > 1秒 | 异常 | 告警 |

###### 检查类型二：普通设备与机房 NTP 同步

> **两种同步方式**：部分服务器运行 NTP/chrony 客户端服务，部分服务器通过每日 cron 任务同步时间。

| 检查项 | 说明 | 检查方法 |
|--------|------|----------|
| **同步方式** | NTP 服务 或 Cron 任务 | 检查运行中服务或 cron 配置 |
| **NTP 客户端状态** | 是否运行 NTP/chrony 服务 | `systemctl status chronyd` |
| **Cron 任务** | 每日同步任务是否存在 | `crontab -l` 或 `/etc/cron.d/` |
| **同步状态** | 是否已同步到 NTP 服务器 | `timedatectl` 或 `chronyc sources` |
| **时间偏差** | 与机房 NTP 服务器偏差 | 与 `192.168.100.10` 对比 |
| **时区** | 时区设置是否正确 | `timedatectl` |

**阈值**：

| 偏差范围 | 判定 | 动作 |
|----------|------|------|
| < 30秒 | 正常 | - |
| 30秒 - 5分钟 | 警告 | 记录 |
| > 5分钟 | 异常 | 告警 |

**同步方式判断**：

```bash
# 判断同步方式
if systemctl is-active chronyd >/dev/null 2>&1; then
    SYNC_METHOD="ntp_service"
elif crontab -l 2>/dev/null | grep -q "ntpdate\|chronyd"; then
    SYNC_METHOD="cron_task"
else
    SYNC_METHOD="none"
fi

echo "同步方式: $SYNC_METHOD"
```

**Cron 任务检查**：

```bash
# 检查每日时间同步 cron 任务
echo "=== Cron 任务检查 ==="

# 检查用户 crontab
crontab -l 2>/dev/null | grep -i ntp

# 检查系统 cron 目录
ls -la /etc/cron.d/ 2>/dev/null | grep -i ntp
ls -la /etc/cron.daily/ 2>/dev/null | grep -i ntp

# 检查最近一次执行时间
if [ -f /var/log/cron ]; then
    grep ntpdate /var/log/cron | tail -5
fi
```

###### 时间同步巡检脚本

```bash
#!/bin/bash
#==============================================================================
# 时间同步巡检脚本
# 支持: NTP 服务器和普通设备
#==============================================================================

# 检测角色
ROLE=$1  # "ntp" 或 "client"

check_ntp_server() {
    # 机房 NTP 服务器检查
    echo "=== NTP 服务器检查 ==="
    
    # 检查上游同步
    echo "上游NTP服务器:"
    ntpdc -p 2>/dev/null || chronyc sources -v 2>/dev/null || echo "无法获取NTP状态"
    
    # 检查时间偏差
    echo "与互联网时间偏差:"
    for server in time.google.com time.cloudflare.com ntp.aliyun.com; do
        offset=$(ntpdate -q $server 2>/dev/null | grep "offset" | awk '{print $NF}')
        echo "  $server: ${offset}s"
    done
    
    # 检查 Stratum
    stratum=$(ntpdc -c sysinfo 2>/dev/null | grep "stratum" | awk '{print $NF}')
    echo "Stratum: $stratum"
}

check_ntp_client() {
    # 普通设备检查
    echo "=== NTP 客户端检查 ==="
    
    # 检查服务状态
    echo "服务状态:"
    systemctl is-active chronyd 2>/dev/null || systemctl is-active ntpd 2>/dev/null || echo "未运行"
    
    # 检查同步状态
    echo "同步状态:"
    timedatectl 2>/dev/null || echo "timedatectl 不可用"
    
    # 检查与 NTP 服务器偏差
    NTP_SERVER="192.168.100.10"  # 机房 NTP 服务器
    offset=$(ntpdate -q $NTP_SERVER 2>/dev/null | grep "offset" | awk '{print $NF}')
    echo "与机房NTP服务器($NTP_SERVER)偏差: ${offset}s"
}

# 主程序
if [ "$ROLE" = "ntp" ]; then
    check_ntp_server
else
    check_ntp_client
fi
```

##### 安全基线检查

> 基于 Linux 系统安全基线标准（RedHat/CentOS），每项检查对应具体的安全加固要求。

| 类别 | 检查项 | 说明 | 检查方法 |
|------|--------|------|----------|
| **账户策略** | root 登录 | 禁止 root 远程登录 | `grep PermitRootLogin /etc/ssh/sshd_config` |
| **账户策略** | 空密码账号 | 不存在空密码账户 | `awk -F: '($2==""){print}' /etc/shadow` |
| **账户策略** | UID 0 账号 | 仅 root 拥有 UID 0 | `awk -F: '($3==0){print}' /etc/passwd` |
| **账户策略** | 系统账号锁定 | 锁定 adm/lp/sync 等系统账号 | `grep "^L" /etc/shadow` |
| **密码策略** | 密码最长天数 | ≤90天 | `grep PASS_MAX_DAYS /etc/login.defs` |
| **密码策略** | 密码最短天数 | ≥5天 | `grep PASS_MIN_DAYS /etc/login.defs` |
| **密码策略** | 密码最小长度 | ≥8位 | `grep PASS_MIN_LEN /etc/login.defs` |
| **密码策略** | 密码加密算法 | SHA512 | `authconfig --test \| grep sha512` |
| **密码策略** | 密码输错锁定 | 锁定失败次数 | `grep pam_tally2 /etc/pam.d/sshd` |
| **登录安全** | SSH 版本 | 仅 SSH v2 | `grep Protocol /etc/ssh/sshd_config` |
| **登录安全** | X11 转发 | 禁用 | `grep X11Forwarding /etc/ssh/sshd_config` |
| **登录安全** | 空闲超时 | TMOUT 设置 | `grep TMOUT /etc/profile` |
| **登录安全** | 历史命令数 | HISTSIZE ≤100 | `grep HISTSIZE /etc/profile` |
| **系统安全** | 启动级别 | init 3 多用户 | `grep initdefault /etc/inittab` |
| **系统安全** | Ctrl+Alt+Del | 已禁用 | `grep ctrlaltdel /etc/inittab` |
| **系统安全** | IPv6 | 已禁用 | `grep NETWORKING_IPV6 /etc/sysconfig/network` |
| **系统安全** | 禁用服务 | 不必要的系统服务 | `chkconfig --list` |
| **网络安全** | 开放端口 | 仅必要端口 | `netstat -tuln` |
| **日志配置** | syslog 发送 | 日志转发到 Splunk | `grep splunk /etc/syslog.conf` |
| **SNMP** | SNMP 配置 | 安全团体字 | `grep com2sec /etc/snmp/snmpd.conf` |

##### 详细检查脚本

```bash
#!/bin/bash
#==============================================================================
# Linux 安全基线检查脚本 (详细版)
# 基于 RedHat/CentOS 安全基线标准
#==============================================================================

check_account_policy() {
    echo "=== 账户策略检查 ==="
    
    # 1. root 远程登录检查
    if grep -q "^PermitRootLogin\s*no" /etc/ssh/sshd_config 2>/dev/null; then
        echo "✓ root远程登录已禁用"
    else
        echo "✗ root远程登录未禁用"
    fi
    
    # 2. 空密码账号检查
    EMPTY=$(awk -F: '($2==""){print}' /etc/shadow 2>/dev/null)
    if [ -z "$EMPTY" ]; then
        echo "✓ 无空密码账号"
    else
        echo "✗ 存在空密码账号: $EMPTY"
    fi
    
    # 3. UID 0 账号检查 (除root)
    UID0=$(awk -F: '($3==0 && $1!="root"){print}' /etc/passwd 2>/dev/null)
    if [ -z "$UID0" ]; then
        echo "✓ 仅root拥有UID0"
    else
        echo "✗ 存在其他UID0账号: $UID0"
    fi
    
    # 4. 系统账号锁定检查
    LOCKED=$(grep -E "^(adm|lp|sync|news|uucp|games|ftp|rpc|rpcuser|nfsnobody|mailnull|gdm)" /etc/passwd | awk -F: '{print $1}')
    echo "系统账号: $LOCKED"
}

check_password_policy() {
    echo "=== 密码策略检查 ==="
    
    # 1. 密码最长天数
    MAX_DAYS=$(grep PASS_MAX_DAYS /etc/login.defs 2>/dev/null | awk '{print $2}')
    if [ "$MAX_DAYS" -le 90 ] 2>/dev/null; then
        echo "✓ 密码最长天数: $MAX_DAYS (≤90)"
    else
        echo "✗ 密码最长天数: $MAX_DAYS (>90)"
    fi
    
    # 2. 密码最短天数
    MIN_DAYS=$(grep PASS_MIN_DAYS /etc/login.defs 2>/dev/null | awk '{print $2}')
    if [ "$MIN_DAYS" -ge 5 ] 2>/dev/null; then
        echo "✓ 密码最短天数: $MIN_DAYS (≥5)"
    else
        echo "✗ 密码最短天数: $MIN_DAYS (<5)"
    fi
    
    # 3. 密码最小长度
    MIN_LEN=$(grep PASS_MIN_LEN /etc/login.defs 2>/dev/null | awk '{print $2}')
    if [ "$MIN_LEN" -ge 8 ] 2>/dev/null; then
        echo "✓ 密码最小长度: $MIN_LEN (≥8)"
    else
        echo "✗ 密码最小长度: $MIN_LEN (<8)"
    fi
    
    # 4. 密码加密算法
    if authconfig --test 2>/dev/null | grep -q "sha512"; then
        echo "✓ 密码加密算法: SHA512"
    else
        echo "✗ 密码加密算法非SHA512"
    fi
    
    # 5. 密码输错锁定 (pam_tally2)
    if grep -q "pam_tally2" /etc/pam.d/sshd 2>/dev/null; then
        echo "✓ SSH登录已配置密码输错锁定"
    else
        echo "✗ SSH登录未配置密码输错锁定"
    fi
}

check_login_security() {
    echo "=== 登录安全检查 ==="
    
    # 1. SSH 版本
    PROTOCOL=$(grep "^Protocol" /etc/ssh/sshd_config 2>/dev/null | awk '{print $2}')
    if [ "$PROTOCOL" = "2" ]; then
        echo "✓ SSH仅允许版本2"
    else
        echo "✗ SSH允许版本: $PROTOCOL"
    fi
    
    # 2. X11 转发
    X11=$(grep "^X11Forwarding" /etc/ssh/sshd_config 2>/dev/null | awk '{print $2}')
    if [ "$X11" = "no" ]; then
        echo "✓ X11转发已禁用"
    else
        echo "✗ X11转发未禁用"
    fi
    
    # 3. 空闲超时
    TMOUT=$(grep "TMOUT" /etc/profile 2>/dev/null | head -1 | cut -d= -f2)
    if [ -n "$TMOUT" ] && [ "$TMOUT" -le 900 ]; then
        echo "✓ 空闲超时: ${TMOUT}秒 (≤900)"
    else
        echo "✗ 空闲超时未设置或过长"
    fi
    
    # 4. 历史命令数
    HISTSIZE=$(grep "HISTSIZE" /etc/profile 2>/dev/null | head -1 | cut -d= -f2)
    if [ -n "$HISTSIZE" ] && [ "$HISTSIZE" -le 100 ]; then
        echo "✓ 历史命令数: $HISTSIZE (≤100)"
    else
        echo "✗ 历史命令数未设置或过长"
    fi
}

check_system_security() {
    echo "=== 系统安全检查 ==="
    
    # 1. 启动级别
    RUNLEVEL=$(grep "^id:" /etc/inittab 2>/dev/null | cut -d: -f2)
    if [ "$RUNLEVEL" = "3" ]; then
        echo "✓ 启动级别: $RUNLEVEL (多用户)"
    else
        echo "✗ 启动级别: $RUNLEVEL (应为3)"
    fi
    
    # 2. Ctrl+Alt+Del
    if ! grep -q "ctrlaltdel" /etc/inittab 2>/dev/null; then
        echo "✓ Ctrl+Alt+Del已禁用"
    else
        echo "✗ Ctrl+Alt+Del未禁用"
    fi
    
    # 3. IPv6
    if grep -q "NETWORKING_IPV6=no" /etc/sysconfig/network 2>/dev/null; then
        echo "✓ IPv6已禁用"
    else
        echo "✗ IPv6未禁用"
    fi
}

check_services() {
    echo "=== 服务检查 ==="
    
    # 检查不必要的服务是否关闭
    UNWANTED="kudzu NetworkManager avahi-daemon bluetooth hidd cups"
    for svc in $UNWANTED; do
        if chkconfig --list $svc 2>/dev/null | grep -q "3:on"; then
            echo "⚠ $svc 服务仍在运行"
        fi
    done
}

check_network_ports() {
    echo "=== 开放端口检查 ==="
    if command -v ss >/dev/null 2>&1; then
        ss -tuln 2>/dev/null | grep LISTEN | awk '{print $5}' | awk -F: '{print $NF}' | sort -n | uniq
    else
        netstat -tuln 2>/dev/null | grep LISTEN | awk '{print $4}' | awk -F: '{print $NF}' | sort -n | uniq
    fi
}

# 主程序
echo "====================================="
echo "Linux 安全基线检查"
echo "====================================="
check_account_policy
check_password_policy
check_login_security
check_system_security
check_services
check_network_ports
echo "====================================="
```

##### 资源使用检查

| 检查项 | 说明 |
|--------|------|
| CPU 使用率 | 当前负载 |
| 内存使用率 | 已用/总量 |
| 磁盘使用率 | 各分区 |

#### 20.9.6 巡检报告

```
每日巡检报告：

┌─────────────────────────────────────────────────────────────────┐
│  巡检时间: 2024-02-14 03:00                    巡检方式: 堡垒机 │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  总体状态: 🟢 正常 (198/200)                                   │
│  ─────────────────────────────────────────────────────────────  │
│                                                                  │
│  时间同步检查:                                                   │
│  ├─ 正常: 195台                                                │
│  ├─ 警告: 3台 (偏差>30秒)                                     │
│  └─ 异常: 2台 (NTP未同步)                                      │
│                                                                  │
│  安全基线检查:                                                   │
│  ├─ 正常: 190台                                                │
│  ├─ 警告: 5台 (开放非必要端口)                                │
│  └─ 异常: 5台 (存在空密码账号 ⚠️)                             │
│                                                                  │
│  需关注:                                                        │
│  ⚠️ server-05: 存在空密码账号 root                            │
│  ⚠️ server-12: NTP 未同步，偏差 120秒                         │
│  ⚠️ server-20: 开放端口 3333 (未知服务)                       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

```bash
#!/bin/bash
# 时间同步检查脚本 (巡检账号执行)

PLATFORM_TIME=$(curl -s https://monitor.example.com/api/v1/time)
CURRENT_TIME=$(date +%s)
PLATFORM_TS=$(date -d "$PLATFORM_TIME" +%s 2>/dev/null || echo $CURRENT_TIME)

DIFF=$((CURRENT_TIME - PLATFORM_TS))
DIFF=${DIFF#-}  # 取绝对值

if [ $DIFF -lt 30 ]; then
    echo "OK: 时间偏差 ${DIFF}秒"
    exit 0
elif [ $DIFF -lt 300 ]; then
    echo "WARNING: 时间偏差 ${DIFF}秒"
    exit 1
else
    echo "CRITICAL: 时间偏差 ${DIFF}秒"
    exit 2
fi
```

#### 20.9.3 Linux 系统安全基线检查

> 定期检查 Linux 系统基础安全设置，发现潜在风险。

##### 检查项目

| 类别 | 检查项 | 检查命令 | 预期 |
|------|--------|----------|------|
| **时间** | 系统时间偏差 | `date` vs 平台时间 | < 30秒 |
| **账号** | root 账号直接登录 | `grep "PermitRootLogin" /etc/ssh/sshd_config` | no |
| **账号** | 空密码账号 | `grep -E "^[^:]+::" /etc/passwd` | 无结果 |
| **账号** | UID 0 账号数量 | `awk -F: '($3==0){print}' /etc/passwd` | 仅 root |
| **权限** | /etc/passwd 权限 | `stat -c %a /etc/passwd` | 644 |
| **权限** | /etc/shadow 权限 | `stat -c %a /etc/shadow` | 400 |
| **权限** | SSH 密钥权限 | `ls -la ~/.ssh/` | 700/600 |
| **服务** | SSH 服务状态 | `systemctl is-active sshd` | active |
| **服务** | 禁用不必要的服务 | `systemctl list-unit-files --state=enabled` | 最少化 |
| **网络** | 开放端口 | `netstat -tuln` | 仅必要端口 |
| **防火墙** | iptables 规则 | `iptables -L -n` | 有规则 |
| **日志** | syslog 配置 | `ls -la /var/log` | 正常轮转 |

##### 安全基线检查脚本

```bash
#!/bin/bash
# Linux 安全基线检查脚本 (巡检账号执行)

RESULTS=()

# 1. 检查 root 禁止直接登录
if grep -q "^PermitRootLogin no" /etc/ssh/sshd_config 2>/dev/null; then
    RESULTS+=("✓ root登录: 已禁用")
else
    RESULTS+=("✗ root登录: 未禁用")
fi

# 2. 检查空密码账号
EMPTY_PASS=$(awk -F: '($2==""){print}' /etc/shadow)
if [ -z "$EMPTY_PASS" ]; then
    RESULTS+=("✓ 空密码: 无")
else
    RESULTS+=("✗ 空密码: $(echo $EMPTY_PASS)")
fi

# 3. 检查 UID 0 账号 (除 root)
UID0_USERS=$(awk -F: '($3==0 && $1!="root"){print}' /etc/passwd)
if [ -z "$UID0_USERS" ]; then
    RESULTS+=("✓ UID0账号: 仅root")
else
    RESULTS+=("✗ UID0账号: $UID0_USERS")
fi

# 4. 检查 /etc/shadow 权限
SHADOW_PERM=$(stat -c %a /etc/shadow 2>/dev/null)
if [ "$SHADOW_PERM" = "400" ]; then
    RESULTS+=("✓ shadow权限: 400")
else
    RESULTS+=("✗ shadow权限: $SHADOW_PERM")
fi

# 5. 检查开放端口 (仅显示监听状态)
LISTEN_PORTS=$(netstat -tuln 2>/dev/null | grep LISTEN | awk '{print $4}' | awk -F: '{print $NF}' | sort -n | uniq)
RESULTS+=("📡 开放端口: $LISTEN_PORTS")

# 6. 检查 SSH 服务状态
SSH_STATUS=$(systemctl is-active sshd 2>/dev/null || echo "unknown")
RESULTS+=("✓ SSH服务: $SSH_STATUS")

# 输出结果
echo "=== 安全基线检查 ==="
for result in "${RESULTS[@]}"; do
    echo "$result"
done
```

#### 20.9.4 巡检执行架构

```
┌─────────────────────────────────────────────────────────────────┐
│                    自动巡检执行架构                                 │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   定时任务 (每日/每小时)                                          │
│        │                                                         │
│        ▼                                                         │
│   ┌─────────────────┐                                            │
│   │   巡检调度器    │                                            │
│   │  (Cron/Airflow) │                                            │
│   └────────┬────────┘                                            │
│            │                                                      │
│            ▼                                                      │
│   ┌─────────────────┐                                            │
│   │  巡检执行器     │                                            │
│   │ (SSH 连接池)   │                                            │
│   └────────┬────────┘                                            │
│            │                                                      │
│    ┌──────┼──────┬──────┐                                       │
│    ▼      ▼      ▼      ▼                                       │
│  主机1  主机2  主机3  ...                                       │
│  (普通账号执行检查)                                               │
│    │      │      │                                               │
│    ▼      ▼      ▼                                               │
│   收集结果 → 格式统一 → 上报平台                                  │
│            │                                                      │
│            ▼                                                      │
│   ┌─────────────────┐                                            │
│   │   结果分析       │                                            │
│   │  (AI 智能分析)  │                                            │
│   └────────┬────────┘                                            │
│            │                                                      │
│            ▼                                                      │
│   ┌─────────────────┐                                            │
│   │  异常 → 告警    │                                            │
│   │  正常 → 归档    │                                            │
│   └─────────────────┘                                            │
└─────────────────────────────────────────────────────────────────┘
```

#### 20.9.5 巡检查询表

```sql
-- 巡检项配置
CREATE TABLE inspection_items (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    category        VARCHAR(20) NOT NULL,
    check_type      VARCHAR(20),
    check_command   TEXT,
    frequency       VARCHAR(20),
    is_enabled      BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 巡检结果
CREATE TABLE inspection_results (
    id              UUID PRIMARY KEY,
    item_id         UUID REFERENCES inspection_items(id),
    asset_id        UUID REFERENCES assets(id),
    check_time      TIMESTAMPTZ NOT NULL,
    result          VARCHAR(20),
    details         JSONB,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- 初始化巡检项
INSERT INTO inspection_items (name, category, check_type, command, frequency) VALUES
-- 时间同步
('系统时间偏差', 'time', 'script', 'check_time.sh', 'hourly'),
('NTP同步状态', 'time', 'command', 'timedatectl status', 'daily'),
-- 安全基线
('root登录检查', 'security', 'command', 'grep PermitRootLogin /etc/ssh/sshd_config', 'daily'),
('空密码账号', 'security', 'command', 'awk -F: "($2==""""){print}" /etc/shadow', 'daily'),
('UID0账号检查', 'security', 'command', 'awk -F: "($3==0 && $1!=""root""){print}" /etc/passwd', 'daily'),
('敏感文件权限', 'security', 'command', 'stat -c "%a %n" /etc/passwd /etc/shadow /etc/sudoers', 'daily'),
('开放端口检查', 'security', 'command', 'netstat -tuln', 'daily'),
('SSH服务状态', 'security', 'command', 'systemctl is-active sshd', 'daily');
```

---

### 20.10 故障复盘管理

```sql
-- 故障复盘表
CREATE TABLE incident_postmortems (
    id              UUID PRIMARY KEY,
    title           VARCHAR(200) NOT NULL,
    occurrence_time TIMESTAMPTZ NOT NULL,
    recovery_time   TIMESTAMPTZ,
    duration        INTEGER,
    impact_scope    VARCHAR(100),
    impact_level    VARCHAR(20),
    root_cause      TEXT NOT NULL,
    root_category   VARCHAR(50),
    timeline        JSONB,
    actions_taken   TEXT[],
    immediate_actions JSONB,
    long_term_actions JSONB,
    lessons_learned TEXT,
    status          VARCHAR(20) DEFAULT 'draft',
    created_at      TIMESTAMP DEFAULT NOW()
);
```

---

### 20.11 应急预案

```sql
-- 应急预案表
CREATE TABLE emergency_plans (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    plan_type       VARCHAR(20) NOT NULL,
    trigger_condition TEXT,
    impact_scope    VARCHAR(100),
    rto_target      INTEGER,
    steps           JSONB NOT NULL,
    primary_contact UUID,
    resources_needed TEXT,
    last_test_at    TIMESTAMP,
    is_active       BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMP DEFAULT NOW()
);
```

---

### 20.12 运维成本管理

```sql
-- 运维成本记录
CREATE TABLE ops_costs (
    id              UUID PRIMARY KEY,
    cost_type       VARCHAR(20) NOT NULL,
    item_name       VARCHAR(100) NOT NULL,
    amount          DECIMAL(15,2) NOT NULL,
    currency        VARCHAR(10) DEFAULT 'CNY',
    cost_period     DATE NOT NULL,
    asset_id        UUID REFERENCES assets(id),
    description     TEXT,
    created_at      TIMESTAMP DEFAULT NOW()
);
```

---

### 21.1 网络关键指标 (KPI)

| 指标 | 说明 | 目标 |
|------|------|------|
| **网络可用性** | 网络通路正常时间比例 | ≥99.99% |
| **平均延迟** | 端到端平均延迟 | ≤5ms |
| **丢包率** | 数据包丢失比例 | ≤0.01% |
| **带宽利用率** | 链路带宽使用比例 | ≤70% |
| **故障响应时间** | 故障发现到响应 | ≤5分钟 |
| **故障修复时间** | 故障发现到修复 | ≤30分钟 |

---

### 21.2 网络割接管理

```sql
-- 网络割接工单
CREATE TABLE network_maintenance (
    id              UUID PRIMARY KEY,
    ticket_id       UUID REFERENCES tickets(id),
    maintenance_type VARCHAR(20),
    affected_devices UUID[],
    downtime_expected INTEGER,
    implementation_plan TEXT,
    rollback_plan    TEXT,
    actual_downtime INTEGER,
    issues_found    TEXT,
    status          VARCHAR(20) DEFAULT 'planned'
);
```

---

## 二十二、实施难度评估与分析

> 作为网络专家和运维团队领导，我对文档中各项功能模块的实现难度做出客观评估，供团队参考。

### 22.1 功能模块难度评级

| 模块 | 难度 | 说明 |
|------|------|------|
| **资产管理 (CRUD)** | ⭐ 简单 | 基础 CRUD，无复杂逻辑 |
| **SNMP 采集** | ⭐ 简单 | 成熟协议，社区资源丰富 |
| **告警阈值触发** | ⭐ 简单 | 规则引擎，易于实现 |
| **工单管理** | ⭐ 简单 | 流程固定，状态机简单 |
| **用户权限 (RBAC)** | ⭐⭐ 中等 | 需要仔细设计权限模型 |
| **自动发现** | ⭐⭐ 中等 | SNMP 扫描可借用开源 |
| **拓扑图** | ⭐⭐ 中等 | 前端可视化有成熟库 |
| **机柜图** | ⭐⭐ 中等 | 2D/3D 渲染有方案 |
| **IPAM** | ⭐⭐ 中等 | IP 池管理逻辑清晰 |
| **DNS 管理** | ⭐⭐ 中等 | 记录管理，绑定操作 |
| **流量采集 (vnStat)** | ⭐⭐ 中等 | 轻量工具，部署简单 |
| **Tripwire 集成** | ⭐⭐ 中等 | 报告解析，AI 分析是重点 |
| **多语言** | ⭐⭐ 中等 | i18next 方案成熟 |
| **日常巡检** | ⭐⭐ 中等 | SSH 执行脚本 |
| **值班排班** | ⭐⭐ 中等 | 排班算法不复杂 |
| **TimescaleDB 时序数据** | ⭐⭐⭐ 较难 | 需要 DBA 经验 |
| **AI 智能分析** | ⭐⭐⭐ 较难 | LLM 调用、Prompt 工程 |
| **向量化知识库** | ⭐⭐⭐ 较难 | Embedding 模型选型 |
| **安全设备集成 (Splunk)** | ⭐⭐⭐ 较难 | API 对接、数据解析 |
| **流量深度分析** | ⭐⭐⭐ 较难 | 需要流量分析经验 |
| **VMware 集成** | ⭐⭐⭐⭐ 难 | vSphere API 复杂 |
| **SolarWinds 迁移** | ⭐⭐⭐⭐ 难 | 数据映射、清洗 |
| **Zabbix 接入** | ⭐⭐⭐⭐ 难 | API 不稳定，需容错 |

### 22.2 技术难点分析

#### 难点一：时序数据存储 (TimescaleDB)

**挑战**：
- 需要 PostgreSQL 运维经验
- 冷热数据分离策略
- 压缩率和性能平衡
- 容量规划

**建议**：
- 先用 PostgreSQL 原生表验证
- 后续平滑迁移到 TimescaleDB
- 参考官方文档的容量规划

#### 难点二：AI 智能分析

**挑战**：
- LLM 调用成本控制
- Prompt 工程需要调优
- 响应时延
- 结果准确性

**建议**：
- 初期用开源模型 (Ollama)
- 明确业务场景再调用 LLM
- 设置超时和降级策略
- 人工抽检验证准确性

#### 难点三：流量深度分析

**挑战**：
- 流量数据量大，存储成本高
- 实时分析性能要求
- 异常检测算法

**建议**：
- 流量采集用轻量方案 (vnStat)
- 异常检测用规则 + 统计
- 大流量场景考虑专用设备

#### 难点四：VMware 集成

**挑战**：
- vSphere API 复杂
- 认证配置繁琐
- 权限模型复杂

**建议**：
- 先做只读集成 (发现资产)
- 后续再扩展控制功能
- 使用官方 SDK

### 22.3 实施优先级建议

> **调整理由**：VMware vCenter 资产读取和 SolarWinds 配置导出应提前到 V1.0，这样可以让监控系统**快速呈现现有资产**，而不是从零开始录入。

基于难度和业务价值，建议分阶段实施：

| 阶段 | 模块 | 预期周期 | 难度 | 理由 |
|------|------|----------|------|------|
| **V1.0** | 资产管理 + 用户认证 + PostgreSQL | 3周 | 简单 | 基础框架 |
| **V1.0** | **VMware vCenter 资产读取** | 1周 | 中等 | **快速导入现有资产** |
| **V1.0** | **SolarWinds 配置导出** | 1周 | 中等 | **复用现有配置** |
| **V1.0** | SNMP 采集 + TimescaleDB | 3周 | 中等 | 数据存储 |
| **V1.0** | 告警通知 + 基础工单 | 2周 | 简单 | 核心功能 |
| **V1.1** | 自动发现 + 拓扑图 | 2周 | 中等 | 资产可视化 |
| **V1.1** | 可视化 + 机柜图 | 2周 | 中等 | 直观展示 |
| **V1.1** | IPAM + DNS 管理 | 2周 | 中等 | 网络管理 |
| **V1.2** | 权限管理 + 审计 | 2周 | 中等 | 安全合规 |
| **V1.2** | 日常巡检 + 值班排班 | 2周 | 中等 | 运维保障 |
| **V2.0** | Zabbix 接入 | 2周 | 难 | 扩展监控 |
| **V2.0** | 流量采集点部署 | 2周 | 中等 | 流量监控 |
| **V2.1** | AI 知识库 + 智能分析 | 3周 | 较难 | 智能化 |
| **V2.1** | 安全设备集成 | 2周 | 较难 | 安全联动 |
| **V3.0** | 流量深度分析 | 3周 | 较难 | 高级功能 |

### V1.0 阶段详解

> V1.0 是**MVP（最小可行产品）**，目标是快速搭建监控系统并导入现有资产。

```
V1.0 实施顺序：

第1周：基础框架
  ├── 环境搭建 (PostgreSQL + TimescaleDB)
  ├── 项目初始化
  └── 用户认证模块

第2周：VMware 资产导入 ⭐ (提前)
  ├── vCenter API 对接
  ├── 资产自动发现
  └── 资产库初始化

第3周：SolarWinds 配置导出 ⭐ (提前)
  ├── Orion API 对接
  ├── 配置数据导出
  └── 映射到本平台

第4-5周：SNMP 采集
  ├── 采集器开发
  ├── 指标入库
  └── 基础告警

第6周：告警 + 工单
  ├── 告警规则引擎
  ├── 通知渠道
  └── 基础工单
```

**提前 VMware + SolarWinds 的好处**：

1. **资产快速入库**：不用手动一条条录入
2. **配置复用**：直接用 SolarWinds 的告警配置
3. **可视化有数据**：机柜图、拓扑图一上来就有东西可看
4. **团队有信心**：两周就能看到成果

### 22.4 风险与应对

| 风险 | 影响 | 应对措施 |
|------|------|----------|
| **需求变更** | 进度延迟 | 敏捷开发，小版本迭代 |
| **技术难点** | 项目延期 | 预留缓冲，技术验证先行 |
| **第三方集成** | 对接不畅 | 早测试，小步推进 |
| **AI 效果不佳** | 功能鸡肋 | 明确场景，持续优化 |
| **数据量大** | 性能瓶颈 | 容量规划，预留扩展 |

### 22.5 技术选型建议

| 场景 | 推荐方案 | 备选 |
|------|----------|------|
| **时序数据库** | TimescaleDB | InfluxDB |
| **AI 模型** | Ollama (本地) | OpenAI API |
| **向量数据库** | Qdrant | Milvus |
| **流量采集** | vnStat + Script | Prometheus |
| **拓扑图** | G6 | JointJS |
| **机柜图** | Three.js | D3.js |

---

## 二十三、现有资源整合规划

> 作为运维团队领导，我梳理了现有可利用的资源，并明确如何与本监控平台整合。

### 23.1 现有资源清单

| 资源 | 用途 | 现状 | 整合方式 |
|------|------|------|----------|
| **Splunk** | 日志分析 | 已部署，日志已接入 | 告警联动 + AI 分析 |
| **科莱流量** | 流量分析 | 已部署，采集点有限 | API 接入 + 扩展采集点 |
| **堡垒机** | SSH 运维 | 已部署，所有 SSH 经此 | 平台集成跳转 |
| **VMware vCenter** | 虚拟化 | 已部署 | 资产自动读取 |
| **Tripwire** | 文件完整性 | 已部署，邮件报告 | 报告入库 + AI 分析 |
| **IPS** | 攻击检测 | 已部署，日志入 Splunk | Splunk 联动告警 |
| **WAF** | Web 防护 | 已部署，日志入 Splunk | Splunk 联动告警 |
| **轻量流量监测点** | 带宽监控 | 未来部署 | vnStat 统一纳管 |

### 23.2 资源整合架构

```
┌─────────────────────────────────────────────────────────────────┐
│                    现有资源整合架构                                 │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    监控平台                               │   │
│  │                                                          │   │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐        │   │
│  │  │资产管理  │ │告警管理 │ │AI分析   │ │统一视图  │        │   │
│  │  └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘        │   │
│  │       │           │           │           │               │   │
│  └───────┼───────────┼───────────┼───────────┼───────────────┘   │
│          │           │           │           │                     │
├──────────┼───────────┼───────────┼───────────┼────────────────────┤
│          │           │           │           │                     │
│  ┌──────┴───────┐   │    ┌─────┴─────┐    │                     │
│  │VMware vCenter │   │    │  Splunk   │    │                     │
│  │  (资产读取)   │   │    │ (日志/告警)│    │                     │
│  └──────┬───────┘   │    └─────┬─────┘    │                     │
│         │              │          │          │                     │
├─────────┼──────────────┼──────────┼──────────┼────────────────────┤
│         │              │          │          │                     │
│  ┌──────┴───────┐   ┌──┴──┐   ┌──┴──┐   ┌──┴──┐                │
│  │  虚拟机/ESXi │   │ IPS │   │ WAF │   │Tripwire              │
│  └──────────────┘   └─────┘   └─────┘   └─────┘                │
│                                                                  │
│  ┌────────────────────────────────────────────────────────┐     │
│  │                    科莱流量系统                          │     │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │     │
│  │  │ 网络采集点   │  │ 服务器端口  │  │ 未来扩展   │   │     │
│  │  │ (现有有限)  │  │   (采样)   │  │(vnStat)    │   │     │
│  │  └──────┬──────┘  └──────┬──────┘  └─────────────┘   │     │
│  └─────────┼─────────────────┼────────────────────────────┘     │
│            │                  │                                  │
│  ┌────────┴─────────────────┴────────┐                          │
│  │           堡垒机                  │                          │
│  │     (所有 SSH 跳转访问)         │                          │
│  └─────────────────────────────────┘                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 23.3 资源联动关系

#### 1. Splunk 整合

| 场景 | 联动方式 |
|------|----------|
| **IPS/WAF 告警** | Splunk 检测 → Webhook → 平台告警 |
| **日志分析** | 平台调用 Splunk API 查询 |
| **AI 辅助** | AI 分析调用 Splunk 日志 |

#### 2. 科莱流量整合

| 场景 | 整合方式 |
|------|----------|
| **现有采集点** | API 接入，纳入统一管理 |
| **扩展采集点** | vnStat 轻量方案，扩展部署 |
| **故障定位** | 流量数据辅助分析 |

#### 3. 堡垒机整合

| 场景 | 整合方式 |
|------|----------|
| **SSH 访问** | 平台跳转 → 堡垒机 → 目标设备 |
| **审计** | 堡垒机自带录像（本平台不重复） |

#### 4. VMware 整合

| 场景 | 整合方式 |
|------|----------|
| **资产发现** | vCenter API 读取虚拟机、ESXi 主机 |
| **监控** | 虚拟化指标采集 |

#### 5. Tripwire 整合

| 场景 | 整合方式 |
|------|----------|
| **报告入库** | 邮件/Syslog 接收报告 |
| **AI 分析** | AI 分析变更，识别风险 |
| **告警** | 高风险变更生成告警 |

#### 6. 轻量流量监测点

| 场景 | 整合方式 |
|------|----------|
| **vnStat 部署** | 每台服务器/交换机部署 |
| **数据上报** | API 推送/ Prometheus 拉取 |
| **统一展示** | 接入平台流量监控 |

### 23.4 数据流汇总

```
数据输入：
─────────────────────────────────────────────────────────────

[VMware vCenter] ──────────▶ [资产模块]
     (资产发现)                (自动入库)

[SolarWinds Orion 9] ──────▶ [资产/配置模块]
     (配置导出)                  (复用)

[科莱流量系统] ────────────▶ [流量分析模块]
     (API接入)                   (统一展示)

[vnStat 监测点] ──────────▶ [流量分析模块]
     (轻量采集)                   (扩展覆盖)

[Tripwire] ───────────────▶ [文件完整性模块]
     (报告入库)                    (AI分析)

[Splunk] ──────────────────▶ [告警模块]
     (IPS/WAF日志)                (联动告警)

[堡垒机] ◀───────────────── [设备访问]
     (SSH跳转)                    (平台发起)
```

### 23.5 整合优先级

| 优先级 | 资源 | 整合目标 | 阶段 |
|--------|------|----------|------|
| **P0** | VMware vCenter | 资产自动读取 | V1.0 |
| **P0** | 科莱流量 | API 接入 | V1.0 |
| **P0** | 堡垒机 | SSH 跳转集成 | V1.0 |
| **P1** | Splunk | IPS/WAF 告警联动 | V1.2 |
| **P1** | Tripwire | 报告入库 + AI 分析 | V2.0 |
| **P2** | 轻量监测点 | vnStat 扩展部署 | V2.0 |

### 23.6 统一视图设计

通过整合后，运维人员在**一个平台**查看所有监控状态：

```
┌─────────────────────────────────────────────────────────────────┐
│                    统一运维视图                                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  仪表盘                                                            │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ 资产总数: 1,234  │ 告警: 5  │  工单: 12  │  可用率: 99.9%│  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─────────────┬─────────────┬─────────────┐                    │
│  │ VM虚拟机    │ 物理服务器  │  网络设备  │                    │
│  │ 450台      │ 120台      │  85台     │                    │
│  └─────────────┴─────────────┴─────────────┘                    │
│                                                                  │
│  监控数据来源:                                                     │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ ☑ SNMP采集    │ ☑ vCenter    │ ☑ 科莱流量  │            │
│  │ ☑ Tripwire   │ ☑ Splunk     │ ☑ vnStat    │            │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  最近告警:                                                        │
│  • 14:30 [WAF] SQL注入攻击 192.168.1.100                       │
│  • 14:15 [IPS] 恶意软件 10.0.0.55                              │
│  • 13:50 [Tripwire] /etc/passwd 变更 ⚠️                        │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

*现有资源整合规划完成 - v1.29*

*文档版本: v1.29*
