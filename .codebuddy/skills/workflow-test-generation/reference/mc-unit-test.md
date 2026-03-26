# MC (Meta Cluster) 单元测试规范

> 适用于 `tdsql/mc/server/` 目录下的 MC 业务代码测试。

---

## 测试目录结构

```
tdsql/mc/server/
├── cluster/                 # Cluster Meta 管理
│   └── meta_test.go
├── conf/                    # 配置管理
├── statistics/              # 统计信息
├── tso/                     # 时间戳服务
├── cluster_info_test.go     # ClusterInfo 测试
├── cluster_info_upgrade_test.go  # 升级逻辑测试
└── ...
```

---

## 文件命名规范

| 类型 | 格式 | 示例 |
|------|------|------|
| 测试文件 | `<feature>_test.go` | `cluster_info_upgrade_test.go`, `meta_test.go` |
| 测试函数 | `Test<Feature>` | `TestHandleUpgrade`, `TestUpdateMCVersion` |
| 子测试 | 描述性命名 | `"Single version upgrade (UNKNOWN → v21.6.1)"` |

---

## 测试框架

使用 Go 标准测试库 + testify：

```go
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

---

## MockMC 测试框架

### 框架位置

`tdsql/pkg/mockmc/mockmc.go` - 提供完整的 MC mock 环境

### 核心组件

| 文件 | 用途 |
|------|------|
| `mockmc.go` | Mock Cluster 创建和初始化 |
| `mockKV.go` | Mock KV 存储（内存实现） |

### 创建 Mock Cluster

```go
import (
    "context"
    "git.woa.com/tdsql3.0/SQLEngine/tdsql/pkg/mockmc"
)

func TestMyFeature(t *testing.T) {
    ctx := context.Background()
    
    // 创建 mock cluster（自动初始化所有依赖）
    mockCluster := mockmc.NewCluster(ctx, nil)
    clusterInfo := mockCluster.MockClusterInfo
    
    // 使用 clusterInfo 进行测试...
}
```

### MockCluster 提供的能力

```go
type Cluster struct {
    BasicCacheInfo   *server.BasicCacheInfo
    IDAllocator      *IDAllocator
    SchOpt           *conf.ScheduleOption
    RepGroupMgr      *server.RepGroupMgr
    Coordinator      *server.Coordinator
    MockClusterInfo  *server.ClusterInfo  // 核心：完整的 ClusterInfo 实例
    KV               *kv.KV               // Mock KV 存储
}
```

### MockClusterInfo 的特性

`mockCluster.MockClusterInfo` 是一个**完整的** `server.ClusterInfo` 实例，包括：

- ✅ **ScheduleOption**: 配置管理（支持 `UpdateScheduleConfig`）
- ✅ **ClusterMeta**: 元数据管理（支持版本管理）
- ✅ **TaskMgr**: 任务管理
- ✅ **RepGroupMgr**: 复制组管理
- ✅ **TSO**: 时间戳服务
- ✅ **KV**: 持久化存储（内存实现）

---

## 测试模式

### 1. testify/suite 模式（推荐用于调度相关、协调器相关的复杂集成测试）

**这是 MC 模块的标准测试框架**。使用 `suite.Suite` 统一管理 `SetupTest` / `TearDownTest` 生命周期，每个测试方法共享相同的 cluster 初始化逻辑但拥有独立的状态。

**标准参考**：`tdsql/mc/server/cluster_test/coordinator_data_locality_test.go`

**测试文件放置**：复杂集成测试放在 `tdsql/mc/server/cluster_test/` 目录下（`package cluster`），简单单元测试放在 `tdsql/mc/server/` 目录下（`package server`）。

```go
package cluster

import (
    "context"
    "testing"

    "git.woa.com/tdsql3.0/SQLEngine/tdsql/mc/server"
    "git.woa.com/tdsql3.0/SQLEngine/tdsql/pkg/mockmc"
    "git.woa.com/tdsql3.0/SQLEngine/tdsql/proto/metarpc"
    "github.com/stretchr/testify/suite"
)

// myFeatureTestSuite defines the test suite for <feature>.
type myFeatureTestSuite struct {
    suite.Suite
    ctx       context.Context
    cancel    context.CancelFunc
    cluster   *mockmc.Cluster
    repGroups []*server.RepGroupInfo
}

// TestMyFeatureTestSuite is the entry point for the test suite.
func TestMyFeatureTestSuite(t *testing.T) {
    suite.Run(t, new(myFeatureTestSuite))
}

// SetupTest initializes a fresh mock cluster before each test method.
func (s *myFeatureTestSuite) SetupTest() {
    s.ctx, s.cancel = context.WithCancel(context.Background())
    s.cluster = mockmc.NewCluster(s.ctx, nil)

    // Configure schedule options
    s.cluster.SetDefaultReplicaCount(3)
    s.cluster.SetHyperNodeLabels([]string{"zone", "rack", "host"})

    // Add nodes with labels
    s.cluster.AddLabelsNode("1", 0, map[string]string{"zone": "z1", "rack": "r1", "host": "h1"})
    s.cluster.AddLabelsNode("2", 0, map[string]string{"zone": "z1", "rack": "r2", "host": "h1"})
    s.cluster.AddLabelsNode("3", 0, map[string]string{"zone": "z1", "rack": "r2", "host": "h2"})

    // Add rep groups
    s.repGroups = nil
    s.repGroups = append(s.repGroups, s.cluster.AddRepGroup(1<<8, mockmc.DefaultRepGroupSize, 1, "1", "2", "3"))
}

// TearDownTest cleans up after each test method (optional).
// func (s *myFeatureTestSuite) TearDownTest() {
//     s.cancel()
// }

// TestBasicScenario is a test method — each method is an independent test case.
func (s *myFeatureTestSuite) TestBasicScenario() {
    rgi := s.cluster.MockClusterInfo.GetRepGroupInfoByID(1 << 8)

    // Add data objects
    dataObject := &metarpc.DataObject{
        DataObjId:          1001,
        DataObjType:        metarpc.DataObjectType_BASE_TABLE,
        DataSpaceType:      metarpc.DataSpaceType_DATA_SPACE_TYPE_USER,
        HasData:            true,
        DataObjIdHierarchy: []uint32{1, 1001},
    }
    s.cluster.AddDataObject(dataObject)

    // Bind regions to rep groups
    s.cluster.AddRegion(rgi.GetRepGroupID(), 1, mockmc.DefaultRegionSize, 1, dataObject)

    // Build rebalance context
    rebalanceCtx := &server.RebalanceContext{
        RoutineCtx: context.TODO(),
        DefaultCrs: server.NewClusterResourcesStats(int(s.cluster.HyperNodesInfo.GetHyperNodeCount())),
    }
    s.cluster.Coordinator.CollectStoresMetrics(rebalanceCtx)

    // Assert
    s.Require().NotNil(rgi)
    s.Equal(uint64(1<<8), rgi.GetRepGroupID())
}
```

**关键模式说明**：

| 要素 | 规范 |
|------|------|
| Suite 命名 | `<feature>TestSuite`（小驼峰） |
| 入口函数 | `Test<Feature>TestSuite(t *testing.T)` |
| `SetupTest` | 每个测试方法前自动调用，创建全新 `mockmc.Cluster` |
| 断言 | `s.Require()` 用于前置条件，`s.Equal()` / `s.True()` 用于验证 |
| 数据对象 | 通过 `s.cluster.AddDataObject()` 注册，通过 `s.cluster.AddRegion()` 绑定到 rep group |
| 配置修改 | 通过 `s.cluster.SchOpt.GetScheduleConfig().<Field> = <value>` 或 `s.cluster.Set*()` |
| RebalanceContext | 通过 `server.NewClusterResourcesStats` + `Coordinator.CollectStoresMetrics` 构建 |

### 2. Table-Driven Tests（推荐用于参数化测试）

```go
func TestMyFeature(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"case1", "input1", "output1"},
        {"case2", "input2", "output2"},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Arrange, Act, Assert
        })
    }
}
```

### 3. Subtests（推荐用于简单场景化测试）

```go
func TestHandleUpgrade(t *testing.T) {
    ctx := context.Background()
    
    t.Run("Single version upgrade", func(t *testing.T) {
        // 独立的测试场景
        mockCluster := mockmc.NewCluster(ctx, nil)
        // ...
    })
    
    t.Run("Already at target version", func(t *testing.T) {
        // 另一个独立场景
        mockCluster := mockmc.NewCluster(ctx, nil)
        // ...
    })
}
```

---

## 断言规范

### testify/assert（非致命错误）

```go
// 用于多个断言，一个失败不影响其他断言执行
assert.NoError(t, err, "Upgrade should succeed")
assert.Equal(t, expected, actual, "Version should match")
assert.True(t, condition, "Config key should exist")
```

### testify/require（致命错误）

```go
// 用于前置条件，失败则终止当前测试
require.NoError(t, err, "Setup must succeed")
require.NotNil(t, obj, "Object must not be nil")
```

### 使用建议

```go
// ✅ 正确：Setup 用 require，验证用 assert
func TestMyFeature(t *testing.T) {
    mockCluster := mockmc.NewCluster(ctx, nil)
    require.NotNil(t, mockCluster, "Cluster creation must succeed")
    
    err := doSomething()
    assert.NoError(t, err)
    assert.Equal(t, expected, actual)
}

// ❌ 错误：Setup 失败后仍继续执行
func TestMyFeature(t *testing.T) {
    mockCluster := mockmc.NewCluster(ctx, nil)
    assert.NotNil(t, mockCluster)  // 应该用 require
    
    mockCluster.DoSomething()  // 可能 panic
}
```

---

## MC 模块单测强制规则

当为 `tdsql/mc/server/` 下的代码生成单元测试时，**必须**：

1. **优先使用 `mockmc.NewCluster()`** 创建 mock 集群环境（放在 `cluster_test/` 目录）
2. **调度/协调器相关测试**使用 `testify/suite` 模式
3. **参考标准文件** `tdsql/mc/server/cluster_test/coordinator_data_locality_test.go` 的测试结构
4. **【铁律】`DataObjIdHierarchy` 最后一个元素必须等于 `DataObjId`**
   - Proto 定义明确规定：`data_obj_id_hierarchy[data_obj_id_hierarchy.size()-1] --> data_obj_id itself`
   - 构造任何 `metarpc.DataObject` 时，`DataObjIdHierarchy` 数组的最后一个元素必须与 `DataObjId` 字段值一致
5. **【铁律】`DataObjIdHierarchy` 的结构必须严格匹配 `DataObjType`**
   - 不同类型的 DataObject，其 hierarchy 长度和语义是固定的：

     | DataObjType | hierarchy 结构 | 长度 |
     |-------------|---------------|------|
     | `BASE_TABLE` | `[dbID, tableID]` | 2 |
     | `BASE_INDEX` | `[dbID, tableID, indexID]` | 3 |
     | `PARTITION_L1` | `[dbID, tableID, partitionID]` | 3 |
     | `PARTITION_L1_INDEX` | `[dbID, tableID, partitionID, indexID]` | 4 |
     | `PARTITION_L2` | `[dbID, tableID, L1_partID, L2_partID]` | 4 |
     | `PARTITION_L2_INDEX` | `[dbID, tableID, L1_partID, L2_partID, indexID]` | 5 |

   - 正确示例：
     ```go
     // BASE_TABLE: [dbID, tableID]
     &metarpc.DataObject{DataObjId: 1001, DataObjType: metarpc.DataObjectType_BASE_TABLE, DataObjIdHierarchy: []uint32{1, 1001}}
     // BASE_INDEX: [dbID, tableID, indexID]
     &metarpc.DataObject{DataObjId: 2001, DataObjType: metarpc.DataObjectType_BASE_INDEX, DataObjIdHierarchy: []uint32{1, 1001, 2001}}
     // PARTITION_L1: [dbID, tableID, partitionID]
     &metarpc.DataObject{DataObjId: 10001, DataObjType: metarpc.DataObjectType_PARTITION_L1, DataObjIdHierarchy: []uint32{1, 1001, 10001}}
     // PARTITION_L1_INDEX: [dbID, tableID, partitionID, indexID]
     &metarpc.DataObject{DataObjId: 10011, DataObjType: metarpc.DataObjectType_PARTITION_L1_INDEX, DataObjIdHierarchy: []uint32{1, 1001, 10001, 10011}}
     // PARTITION_L2: [dbID, tableID, L1_partID, L2_partID]
     &metarpc.DataObject{DataObjId: 100001, DataObjType: metarpc.DataObjectType_PARTITION_L2, DataObjIdHierarchy: []uint32{1, 1001, 10001, 100001}}
     // PARTITION_L2_INDEX: [dbID, tableID, L1_partID, L2_partID, indexID]
     &metarpc.DataObject{DataObjId: 100011, DataObjType: metarpc.DataObjectType_PARTITION_L2_INDEX, DataObjIdHierarchy: []uint32{1, 1001, 10001, 100001, 100011}}
     ```
   - 错误示例：
     ```go
     // ❌ 错误：PARTITION_L1 的 hierarchy 应该是 3 个元素 [dbID, tableID, partitionID]，不是 2 个
     &metarpc.DataObject{DataObjId: 10001, DataObjType: metarpc.DataObjectType_PARTITION_L1, DataObjIdHierarchy: []uint32{1, 10001}}
     // ❌ 错误：PARTITION_L1_INDEX 缺少 partitionID 层级
     &metarpc.DataObject{DataObjId: 10011, DataObjType: metarpc.DataObjectType_PARTITION_L1_INDEX, DataObjIdHierarchy: []uint32{1, 1001, 10011}}
     ```
6. **【铁律】`IndexType` 只能出现在索引类型的 DataObject 上**
   - 只有 `DataObjType` 为 `BASE_INDEX`、`PARTITION_L1_INDEX`、`PARTITION_L2_INDEX` 的 DataObject 才应该设置 `IndexType` 字段
   - `BASE_TABLE`、`PARTITION_L1`、`PARTITION_L2` 等非索引类型**不应该**设置 `IndexType`
   - 同时，所有索引类型的 DataObject **必须**显式设置 `DataObjType`，不能依赖零值（零值是 `BASE_TABLE`）
   - 正确示例：
     ```go
     // ✅ 正确：BASE_INDEX 设置 IndexType
     &metarpc.DataObject{DataObjId: 201, DataObjType: metarpc.DataObjectType_BASE_INDEX, IndexType: metarpc.IndexType_IT_UNIQUE, ...}
     // ✅ 正确：PARTITION_L1 不设置 IndexType
     &metarpc.DataObject{DataObjId: 300, DataObjType: metarpc.DataObjectType_PARTITION_L1, HasData: true, ...}
     ```
   - 错误示例：
     ```go
     // ❌ 错误：BASE_TABLE 不应该有 IndexType
     &metarpc.DataObject{DataObjId: 100, DataObjType: metarpc.DataObjectType_BASE_TABLE, IndexType: metarpc.IndexType_IT_UNIQUE, ...}
     // ❌ 错误：PARTITION_L1 不应该有 IndexType
     &metarpc.DataObject{DataObjId: 300, DataObjType: metarpc.DataObjectType_PARTITION_L1, IndexType: metarpc.IndexType_IT_PRIMARY, ...}
     // ❌ 错误：索引对象缺少 DataObjType（默认零值是 BASE_TABLE，语义错误）
     &metarpc.DataObject{DataObjId: 201, IndexType: metarpc.IndexType_IT_UNIQUE, ...}
     ```

---

## 常见测试场景

### 场景1: 测试配置更新

```go
func TestConfigUpdate(t *testing.T) {
    ctx := context.Background()
    mockCluster := mockmc.NewCluster(ctx, nil)
    clusterInfo := mockCluster.MockClusterInfo
    
    // Arrange: 设置初始配置
    err := clusterInfo.UpdateScheduleConfig(false, &conf.ScheduleConfigUpdateCtx{
        Key:   "my-config-key",
        Value: "1",
    })
    require.NoError(t, err)
    
    // Act: 执行业务逻辑
    err = myBusinessLogic(clusterInfo)
    
    // Assert: 验证配置变化
    value, ok := clusterInfo.GetScheduleConfigByName("my-config-key")
    assert.True(t, ok, "Config key should exist")
    assert.Equal(t, "0", value, "Config should be disabled")
}
```

### 场景2: 测试版本管理

```go
func TestVersionUpgrade(t *testing.T) {
    ctx := context.Background()
    mockCluster := mockmc.NewCluster(ctx, nil)
    clusterInfo := mockCluster.MockClusterInfo
    clusterMeta := clusterInfo.GetClusterMeta()
    
    // Arrange: 设置初始版本
    err := clusterMeta.UpdateMCVersion(ctx, metarpc.MCVersionEnum_MC_VERSION_UNKNOWN, false)
    require.NoError(t, err)
    
    // Act: 触发升级
    err = clusterInfo.CheckAndHandleUpgrade(ctx)
    
    // Assert: 验证版本更新
    assert.NoError(t, err)
    assert.Equal(t, metarpc.MCVersionEnum_MC_VERSION_21_6_1, clusterMeta.GetMCVersion())
}
```

### 场景3: 测试幂等性

```go
func TestIdempotency(t *testing.T) {
    ctx := context.Background()
    mockCluster := mockmc.NewCluster(ctx, nil)
    clusterInfo := mockCluster.MockClusterInfo
    
    // Arrange: 准备初始状态
    setupInitialState(clusterInfo)
    
    // Act: 执行多次相同操作
    for i := 0; i < 3; i++ {
        err := myIdempotentOperation(clusterInfo)
        assert.NoError(t, err, "Operation %d should succeed", i+1)
    }
    
    // Assert: 验证最终状态正确
    assertFinalState(t, clusterInfo)
}
```

### 场景4: 测试错误处理

```go
func TestErrorHandling(t *testing.T) {
    ctx := context.Background()
    
    t.Run("Invalid input", func(t *testing.T) {
        mockCluster := mockmc.NewCluster(ctx, nil)
        clusterInfo := mockCluster.MockClusterInfo
        
        // Act: 传入非法参数
        err := myFunction(clusterInfo, invalidInput)
        
        // Assert: 验证错误返回
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "invalid")
    })
}
```

---

## MockMC 高级用法

### 自定义 KV 存储

```go
// 使用自定义 KV（例如注入错误）
customKV := mockmc.NewMockKV(nil)
mockCluster := mockmc.NewCluster(ctx, kv.NewKV(customKV))
```

### 添加 Mock 节点

```go
// 添加 HyperNode（按 RepGroup 数量统计容量）
mockCluster.AddRepGroupNode("node1", 10)  // 10 个 RepGroup
mockCluster.AddLeaderNode("node2", 5)      // 5 个 Leader

// 添加带标签的节点（调度均衡测试必需）
mockCluster.AddLabelsNode("node3", 10, map[string]string{
    "zone": "zone1",
    "rack": "rack1",
    "host": "host1",
})

// 设置节点属性
mockCluster.SetHyperNodeProperty("node1", []*metarpc.NodeLabel{{Key: "k", Value: "v"}})
mockCluster.SetHyperNodeReadOnly("node1")
mockCluster.SetHyperNodeReadWrite("node1")
```

### 添加 RepGroup 和 Region

```go
// 添加 RepGroup（leader + followers，自动更新 node stats）
rgi := mockCluster.AddRepGroup(
    repGroupID,                // uint64, 推荐用 N<<8 避免冲突
    size,                      // uint64, 字节数
    keys,                      // uint64, key 数量
    leaderNodeName,            // string
    followerNodeNames...,      // ...string
)

// 添加 Region（绑定到已有 RepGroup + DataObject）
regionMeta := mockCluster.AddRegion(repGroupID, regionID, size, keys, dataObject)
```

### DataObject 层级体系

MC 中的 DataObject 通过 `DataObjIdHierarchy` 表达层级关系：

```go
// 【铁律】hierarchy 结构必须严格匹配 DataObjType，且最后一个元素必须等于 DataObjId
//
// | DataObjType          | hierarchy                                        |
// |----------------------|--------------------------------------------------|
// | BASE_TABLE           | [dbID, tableID]                                  |
// | BASE_INDEX           | [dbID, tableID, indexID]                          |
// | PARTITION_L1         | [dbID, tableID, partitionID]                     |
// | PARTITION_L1_INDEX   | [dbID, tableID, partitionID, indexID]             |
// | PARTITION_L2         | [dbID, tableID, L1_partID, L2_partID]            |
// | PARTITION_L2_INDEX   | [dbID, tableID, L1_partID, L2_partID, indexID]   |

// 1. 普通表
baseTable := &metarpc.DataObject{
    DataObjId:          1001,
    DataObjType:        metarpc.DataObjectType_BASE_TABLE,
    DataSpaceType:      metarpc.DataSpaceType_DATA_SPACE_TYPE_USER,
    HasData:            true,
    DataObjIdHierarchy: []uint32{1, 1001},
}

// 2. 分区表（BASE_TABLE 本身 HasData=false）
partitionParent := &metarpc.DataObject{
    DataObjId:          1001,
    DataObjType:        metarpc.DataObjectType_BASE_TABLE,
    DataSpaceType:      metarpc.DataSpaceType_DATA_SPACE_TYPE_USER,
    HasData:            false,  // 分区表的 base 没有数据
    DataObjIdHierarchy: []uint32{1, 1001},
}
partition := &metarpc.DataObject{
    DataObjId:          10001,
    DataObjType:        metarpc.DataObjectType_PARTITION_L1,
    DataSpaceType:      metarpc.DataSpaceType_DATA_SPACE_TYPE_USER,
    HasData:            true,
    DataObjIdHierarchy: []uint32{1, 1001, 10001},
}

// 3. 子分区表
subPartition := &metarpc.DataObject{
    DataObjId:          100001,
    DataObjType:        metarpc.DataObjectType_PARTITION_L2,
    DataSpaceType:      metarpc.DataSpaceType_DATA_SPACE_TYPE_USER,
    HasData:            true,
    DataObjIdHierarchy: []uint32{1, 1001, 10001, 100001},
}

// 4. 索引（挂在分区下）
partitionIndex := &metarpc.DataObject{
    DataObjId:          10011,
    DataObjType:        metarpc.DataObjectType_PARTITION_L1_INDEX,
    DataSpaceType:      metarpc.DataSpaceType_DATA_SPACE_TYPE_USER,
    HasData:            true,
    DataObjIdHierarchy: []uint32{1, 1001, 10001, 10011},
}

// 注册到集群
mockCluster.AddDataObject(baseTable)
mockCluster.AddDataObject(partition)
// ...
```

### 修改调度配置

```go
// 方法 1: 直接修改 ScheduleConfig（推荐，简单直接）
cfg := mockCluster.SchOpt.GetScheduleConfig()
cfg.HashTablePatrolEnabled = 1
cfg.MergeRegionEnabled = 1
cfg.SplitRepGroupSizeThreshold = 2 * 1024 * 1024 * 1024

// 方法 2: 使用 Cluster 提供的 Set* 辅助方法
mockCluster.SetDefaultReplicaCount(3)
mockCluster.SetHyperNodeLabels([]string{"zone", "rack", "host"})
mockCluster.SetSplitMergeInterval(5 * time.Minute)

// 方法 3: 使用 UpdateScheduleConfig（会走 KV 持久化路径）
err := mockCluster.MockClusterInfo.UpdateScheduleConfig(false, &conf.ScheduleConfigUpdateCtx{
    Key:   "my-config-key",
    Value: "1",
})
```

### 构建 RebalanceContext（调度测试必需）

```go
rebalanceCtx := &server.RebalanceContext{
    RoutineCtx:  context.TODO(),
    DefaultCrs:  server.NewClusterResourcesStats(int(mockCluster.HyperNodesInfo.GetHyperNodeCount())),
    ColumnarCrs: nil,
    LogOnlyCrs:  nil,
}
mockCluster.Coordinator.CollectStoresMetrics(rebalanceCtx)
```

---

## 测试用例结构（AAA 模式）

```go
func TestFeature(t *testing.T) {
    t.Run("Scenario description", func(t *testing.T) {
        // Arrange（准备）
        ctx := context.Background()
        mockCluster := mockmc.NewCluster(ctx, nil)
        clusterInfo := mockCluster.MockClusterInfo
        
        // 设置前置条件
        setupPreconditions(clusterInfo)
        
        // Act（执行）
        err := executeBusinessLogic(clusterInfo)
        
        // Assert（断言）
        assert.NoError(t, err)
        assertExpectedState(t, clusterInfo)
    })
}
```

---

## 运行测试

### 运行单个测试文件

```bash
cd tdsql/mc/server
go test -v -run TestHandleUpgrade
```

### 运行特定子测试

```bash
go test -v -run "TestHandleUpgrade/Single_version_upgrade"
```

### 运行所有 MC 测试

```bash
cd tdsql/mc/server
go test -v ./...
```

### 带覆盖率运行

```bash
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## 最佳实践

### 1. 每个子测试独立创建 MockCluster

```go
// ✅ 正确：每个子测试独立
func TestMyFeature(t *testing.T) {
    t.Run("case1", func(t *testing.T) {
        mockCluster := mockmc.NewCluster(ctx, nil)
        // ...
    })
    
    t.Run("case2", func(t *testing.T) {
        mockCluster := mockmc.NewCluster(ctx, nil)
        // ...
    })
}

// ❌ 错误：子测试共享状态
func TestMyFeature(t *testing.T) {
    mockCluster := mockmc.NewCluster(ctx, nil)  // 共享实例
    
    t.Run("case1", func(t *testing.T) {
        // case1 的修改会影响 case2
    })
}
```

### 2. UpdateScheduleConfig 使用 flush=false

```go
// ✅ 正确：单元测试不需要持久化
err := clusterInfo.UpdateScheduleConfig(false, &conf.ScheduleConfigUpdateCtx{
    Key:   "my-key",
    Value: "my-value",
})

// ❌ 避免：flush=true 会尝试写入 etcd（在 mock 环境中已支持，但非必要）
err := clusterInfo.UpdateScheduleConfig(true, &conf.ScheduleConfigUpdateCtx{...})
```

### 3. 使用描述性的子测试名称

```go
// ✅ 正确：清晰描述测试场景
t.Run("Single version upgrade (UNKNOWN → v21.6.1)", func(t *testing.T) {})
t.Run("Idempotency: executing twice has no side effects", func(t *testing.T) {})

// ❌ 错误：名称不清晰
t.Run("test1", func(t *testing.T) {})
t.Run("upgrade", func(t *testing.T) {})
```

### 4. 测试边界条件

参考 `boundary-checklist.md`，特别关注：
- 版本边界（UNKNOWN、CURRENT、未来版本）
- 配置存在性（key 存在/不存在）
- 重复执行（幂等性验证）
- 错误注入

### 5. 添加测试注释说明

```go
// TestUpgradeIdempotency verifies that upgrade logic can be safely executed multiple times
// This is critical for scenarios where MC crashes after executing upgrade logic
// but before updating meta.mc_version
func TestUpgradeIdempotency(t *testing.T) {
    // ...
}
```

### 6. 用 ASCII 树形注释画清楚 DataObject 层级关系

当测试用例需要构建 `dataObjectsInfo`（tiered map）和 `routes` 等多层数据结构时，**必须**在测试用例开头用 ASCII 树形注释画出完整的表结构关系图，让读者无需逐行阅读代码即可理解数据布局。

**格式规范**：

```
// Table structure (<描述>, db=<dbID>):
//
//   <DataObjType> <DataObjId> [hierarchy...]       ← 说明文字
//   ├── <DataObjType> <DataObjId> [hierarchy...]   ← 说明文字, RG <id> (node-<x>)
//   └── <DataObjType> <DataObjId> [hierarchy...]   ← 说明文字, RG <id> (node-<x>)
//
// <预期结果总结>
```

**要素**：
- 第一行标注 `Table structure`，括号内说明是非分区/单分区/多分区，以及 dbID
- 每个节点包含：`DataObjType`、`DataObjId`、`[hierarchy]` 数组、所在 RG 和 Node
- 使用 `├──`/`└──` 展示父子关系，分区表用嵌套缩进
- 末尾总结该用例的预期行为

**示例 — 非分区表**：

```go
t.Run("non-partitioned table — all RGs colocated", func(t *testing.T) {
    // Table structure (non-partitioned, db=1):
    //
    //   BASE_TABLE 100 [1, 100]           ← table meta, HasData=true
    //   ├── BASE_INDEX 101 [1, 100, 101]  ← existing data element, RG 10 (node-a)
    //   ├── BASE_INDEX 102 [1, 100, 102]  ← existing data element, RG 20 (node-b)
    //   ├── BASE_INDEX 201 [1, 100, 201]  ← new UNIQUE index,     RG 10 (node-a)
    //   └── BASE_INDEX 202 [1, 100, 202]  ← new UNIQUE index,     RG 20 (node-b)
    //
    // After constraint creation, all RGs colocated: {10, 20}.
    tableObjID := uint32(100)
    // ...
})
```

**示例 — 分区表（多分区）**：

```go
t.Run("partitioned table with two partitions", func(t *testing.T) {
    // Table structure (two-partition, db=1):
    //
    //   BASE_TABLE 100 [1, 100]                          ← table meta, HasData=false
    //   ├── PARTITION_L1 300 [1, 100, 300]               ← partition 300 (implicit)
    //   │   ├── elem 101 [1, 100, 300, 101]              ← existing data, RG 10 (node-a)
    //   │   ├── elem 102 [1, 100, 300, 102]              ← existing data, RG 20 (node-b)
    //   │   └── PARTITION_L1_INDEX 201 [1, 100, 300, 201] ← new UNIQUE index, RG 10 (node-a)
    //   └── PARTITION_L1 400 [1, 100, 400]               ← partition 400 (implicit)
    //       ├── elem 103 [1, 100, 400, 103]              ← existing data, RG 30 (node-c)
    //       ├── elem 104 [1, 100, 400, 104]              ← existing data, RG 40 (node-d)
    //       └── PARTITION_L1_INDEX 202 [1, 100, 400, 202] ← new UNIQUE index, RG 30 (node-c)
    //
    // After constraint creation:
    //   - partition 300 colocates {RG 10, RG 20}
    //   - partition 400 colocates {RG 30, RG 40}
    tableObjID := uint32(100)
    // ...
})
```

---

## MockKV 存储特性

### 内存实现

MockKV 是完全内存的 KV 存储实现，支持：

- ✅ `Save()`: 保存键值对
- ✅ `Load()`: 加载键值对
- ✅ `Remove()`: 删除键值对
- ✅ `LoadRange()`: 范围查询
- ✅ 事务支持（通过 `RunInTxn`）

### KV 操作示例

```go
mockCluster := mockmc.NewCluster(ctx, nil)
kv := mockCluster.KV

// 保存数据
err := kv.Save("/my/key", "my-value")
require.NoError(t, err)

// 加载数据
value, err := kv.Load("/my/key")
require.NoError(t, err)
assert.Equal(t, "my-value", value)

// 删除数据
err = kv.Remove("/my/key")
require.NoError(t, err)
```

---

## 新测试文件模板

### 模板 A: testify/suite 模式（推荐 - 调度/协调器测试）

测试文件放在 `tdsql/mc/server/cluster_test/` 目录下：

```go
package cluster

import (
    "context"
    "testing"

    "git.woa.com/tdsql3.0/SQLEngine/tdsql/mc/server"
    "git.woa.com/tdsql3.0/SQLEngine/tdsql/pkg/mockmc"
    "git.woa.com/tdsql3.0/SQLEngine/tdsql/proto/metarpc"
    "github.com/stretchr/testify/suite"
)

type myFeatureTestSuite struct {
    suite.Suite
    ctx       context.Context
    cancel    context.CancelFunc
    cluster   *mockmc.Cluster
    repGroups []*server.RepGroupInfo
}

func TestMyFeatureTestSuite(t *testing.T) {
    suite.Run(t, new(myFeatureTestSuite))
}

func (s *myFeatureTestSuite) SetupTest() {
    s.ctx, s.cancel = context.WithCancel(context.Background())
    s.cluster = mockmc.NewCluster(s.ctx, nil)

    s.cluster.SetDefaultReplicaCount(3)
    s.cluster.SetHyperNodeLabels([]string{"zone", "rack", "host"})

    s.cluster.AddLabelsNode("1", 0, map[string]string{"zone": "z1", "rack": "r1", "host": "h1"})
    s.cluster.AddLabelsNode("2", 0, map[string]string{"zone": "z1", "rack": "r2", "host": "h1"})
    s.cluster.AddLabelsNode("3", 0, map[string]string{"zone": "z1", "rack": "r2", "host": "h2"})

    s.repGroups = nil
    s.repGroups = append(s.repGroups, s.cluster.AddRepGroup(1<<8, mockmc.DefaultRepGroupSize, 1, "1", "2", "3"))
}

func (s *myFeatureTestSuite) TestBasicScenario() {
    rgi := s.cluster.MockClusterInfo.GetRepGroupInfoByID(1 << 8)

    dataObject := &metarpc.DataObject{
        DataObjId:          1001,
        DataObjType:        metarpc.DataObjectType_BASE_TABLE,
        DataSpaceType:      metarpc.DataSpaceType_DATA_SPACE_TYPE_USER,
        HasData:            true,
        DataObjIdHierarchy: []uint32{1, 1001},
    }
    s.cluster.AddDataObject(dataObject)
    s.cluster.AddRegion(rgi.GetRepGroupID(), 1, mockmc.DefaultRegionSize, 1, dataObject)

    rebalanceCtx := &server.RebalanceContext{
        RoutineCtx: context.TODO(),
        DefaultCrs: server.NewClusterResourcesStats(int(s.cluster.HyperNodesInfo.GetHyperNodeCount())),
    }
    s.cluster.Coordinator.CollectStoresMetrics(rebalanceCtx)

    // Business logic & assertions
    s.Require().NotNil(rgi)
}
```

// Helper functions
func assertExpectedBehavior(t *testing.T, clusterInfo *server.ClusterInfo) {
    value, ok := clusterInfo.GetScheduleConfigByName("my-key")
    assert.True(t, ok)
    assert.Equal(t, "expected-value", value)
}
```

---

## 实际案例

### 案例 1: 调度器集成测试（testify/suite 模式 - 标准参考）

**标准参考文件**：`tdsql/mc/server/cluster_test/coordinator_data_locality_test.go`

此文件展示了完整的 MC 调度相关测试范式：

```go
// 参考 coordinator_data_locality_test.go 的核心模式：
// 1. 使用 suite.Suite 管理测试生命周期
// 2. SetupTest 中统一创建 mockmc.Cluster、配置节点和 RepGroup
// 3. 每个测试方法内独立创建 DataObject 和 Region
// 4. 通过 RebalanceContext + CollectStoresMetrics 构建调度上下文
// 5. 验证调度决策结果（TaskInfo、JobsTopologyGraph 等）
```

**关键模式提取**：

```go
// Step 1: Suite 定义 — 包含 cluster 和 repGroups 作为共享状态
type mergePrimaryKeysTestSuite struct {
    suite.Suite
    ctx       context.Context
    cancel    context.CancelFunc
    cluster   *mockmc.Cluster
    repGroups []*server.RepGroupInfo
    pm        *server.CheckPrimaryKeyManager
}

// Step 2: SetupTest — 每个测试方法前重建环境
func (suite *mergePrimaryKeysTestSuite) SetupTest() {
    suite.ctx, suite.cancel = context.WithCancel(context.Background())
    suite.cluster = mockmc.NewCluster(suite.ctx, nil)

    // 配置调度参数
    suite.cluster.SetDefaultReplicaCount(3)
    suite.cluster.SetHyperNodeLabels([]string{"zone", "rack", "host"})
    suite.cluster.SchOpt.GetScheduleConfig().MergeRegionEnabled = 1

    // 添加节点
    suite.cluster.AddLabelsNode("1", 0, map[string]string{"zone": "z1", "rack": "r1", "host": "h1"})
    suite.cluster.AddLabelsNode("2", 0, map[string]string{"zone": "z1", "rack": "r2", "host": "h1"})
    suite.cluster.AddLabelsNode("3", 0, map[string]string{"zone": "z1", "rack": "r2", "host": "h2"})

    // 添加基础 RepGroup
    suite.repGroups = nil
    suite.repGroups = append(suite.repGroups, suite.cluster.AddRepGroup(1<<8, mockmc.DefaultRepGroupSize, 1, "1", "2"))

    // 初始化被测对象
    suite.pm = &server.CheckPrimaryKeyManager{CInfo: suite.cluster.MockClusterInfo}
}

// Step 3: 测试方法 — 构建场景并验证
func (suite *mergePrimaryKeysTestSuite) TestBasic() {
    // 额外的 RepGroup
    suite.repGroups = append(suite.repGroups,
        suite.cluster.AddRepGroup(5<<8, mockmc.DefaultRepGroupSize, 100, "1", "2", "3"))
    suite.repGroups = append(suite.repGroups,
        suite.cluster.AddRepGroup(6<<8, mockmc.DefaultRepGroupSize*2, 200, "3", "1", "2"))

    rgi := suite.cluster.MockClusterInfo.GetRepGroupInfoByID(5 << 8)
    rgi2 := suite.cluster.MockClusterInfo.GetRepGroupInfoByID(6 << 8)

    // 注册 DataObject（带层级关系）
    dataObject := &metarpc.DataObject{
        DataObjId:          1001,
        DataObjType:        metarpc.DataObjectType_BASE_TABLE,
        DataSpaceType:      metarpc.DataSpaceType_DATA_SPACE_TYPE_USER,
        HasData:            true,
        DataObjIdHierarchy: []uint32{1, 1001},
    }
    suite.cluster.AddDataObject(dataObject)

    // 绑定 Region 到 RepGroup
    suite.cluster.AddRegion(rgi.GetRepGroupID(), 1, mockmc.DefaultRegionSize, 1, dataObject)
    suite.cluster.AddRegion(rgi2.GetRepGroupID(), 2, mockmc.DefaultRegionSize*2, 2, dataObject)

    // 构建调度上下文
    rebalanceCtx := &server.RebalanceContext{
        RoutineCtx: context.TODO(),
        DefaultCrs: server.NewClusterResourcesStats(int(suite.cluster.HyperNodesInfo.GetHyperNodeCount())),
    }
    suite.cluster.Coordinator.CollectStoresMetrics(rebalanceCtx)

    // 执行并验证
    taskInfos, err := suite.pm.CheckDetachedUserPrimaryKeyRegions(context.Background(), rebalanceCtx.DefaultCrs)
    suite.Require().Nil(err)
    suite.Require().NotNil(taskInfos)
    suite.Require().Len(taskInfos, 1)
}
```

### 案例 2: 简单单元测试（subtest 模式）

完整示例请参考 `tdsql/mc/server/cluster_info_upgrade_test.go`：

```go
func TestHandleUpgrade(t *testing.T) {
    ctx := context.Background()

    t.Run("Single version upgrade (UNKNOWN → v21.6.1)", func(t *testing.T) {
        mockCluster := mockmc.NewCluster(ctx, nil)
        clusterInfo := mockCluster.MockClusterInfo
        clusterMeta := clusterInfo.GetClusterMeta()

        err := clusterMeta.UpdateMCVersion(ctx, metarpc.MCVersionEnum_MC_VERSION_UNKNOWN, false)
        require.NoError(t, err)

        err = clusterInfo.CheckAndHandleUpgrade(ctx)

        assert.NoError(t, err, "Upgrade should succeed")
        assert.Equal(t, metarpc.MCVersionEnum_MC_VERSION_21_6_1, clusterMeta.GetMCVersion())
    })
}
```

---

## 常见问题

### Q1: MockCluster 创建失败？

```go
// 确保传入正确的 context
ctx := context.Background()
mockCluster := mockmc.NewCluster(ctx, nil)
require.NotNil(t, mockCluster)
```

### Q2: UpdateScheduleConfig 报错 "kv is nil"？

最新的 mockmc 框架已修复此问题。如遇到，确保使用最新代码。

### Q3: 如何测试持久化逻辑？

```go
// 方法1: 直接验证 KV 中的值
value, err := mockCluster.KV.Load("/path/to/key")
assert.NoError(t, err)
assert.Equal(t, expectedValue, value)

// 方法2: 验证内存状态（对于 flush=false 的场景）
value, ok := clusterInfo.GetScheduleConfigByName("my-key")
assert.True(t, ok)
```

### Q4: 如何模拟并发场景？

```go
import "sync"

func TestConcurrentAccess(t *testing.T) {
    mockCluster := mockmc.NewCluster(ctx, nil)
    clusterInfo := mockCluster.MockClusterInfo
    
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            err := doSomething(clusterInfo, id)
            assert.NoError(t, err)
        }(i)
    }
    wg.Wait()
}
```

### Q5: server 包内的测试 import mockmc 报循环依赖？

```
import cycle not allowed in test:
    server → ... → mockmc → server
```

**原因**：`mockmc` 包导入了 `server` 包，`server` 包内的测试文件再导入 `mockmc` 就形成循环。

**解决方案**（按优先级选择）：

| 选择 | 适用场景 | 做法 |
|------|----------|------|
| **移到 `cluster_test/`** | 测试只需要导出 API | 改为 `package cluster`，使用 `mockmc` |
| **留在 `server/`** | 测试需要未导出字段/方法 | 手动构建 `ClusterInfo`，不用 `mockmc` |
| **添加 `ForTest` 包装** | 需要从 `cluster_test` 访问个别未导出方法 | 在 `server` 包添加 `XxxForTest()` 导出包装 |

### Q6: 手动构建 ClusterInfo 时 panic: nil pointer？

最常见原因：**初始化顺序错误**。

```go
// ❌ 错误：TaskMgr 在 Coordinator 之前
cInfo.TaskMgr = cInfo.NewClusterTaskManager(kv_)  // panic: NewPartitionScatter 访问 nil cInfo.Coordinator.Ctx
cInfo.Coordinator = NewCoordinator(cInfo)

// ✅ 正确：Coordinator 先于 TaskMgr
cInfo.Coordinator = NewCoordinator(cInfo)
cInfo.TaskMgr = cInfo.NewClusterTaskManager(kv_)
```

**根因**：`NewClusterTaskManager` 内部调用 `NewPartitionScatter(c)`，后者在初始化时访问 `c.Coordinator.Ctx`。

**完整正确的初始化顺序**：
1. `BasicCacheInfo`
2. `conf.NewConfig` + `Adjust`
3. `EventBus` + `RepGroupMgrs`
4. `ScheduleOption`
5. **`Coordinator`** ← 必须在 TaskMgr 之前
6. **`TaskMgr`**

### Q7: 白盒测试应该用什么测试模式？

**推荐使用 `testify/suite`**，即使是在 `server` 包内。好处：
- `SetupTest` / `TearDownTest` 统一管理 `ClusterInfo` 和 `context` 生命周期
- 每个测试方法自动获得干净的初始状态
- Helper 操作封装为 suite 方法，避免参数传递
- 断言语法简洁（`s.Equal(...)` 替代 `assert.Equal(t, ...)`）

参考模板 B。

---

## 测试覆盖率目标

- 核心业务逻辑：≥ 80%
- 关键路径（如升级、配置变更）：≥ 90%
- 错误处理分支：≥ 70%

---

## 参考资源

- **MockMC 框架实现**：`tdsql/pkg/mockmc/mockmc.go`
- **标准参考（Suite 模式）**：`tdsql/mc/server/cluster_test/coordinator_data_locality_test.go`
- 升级逻辑测试示例：`tdsql/mc/server/cluster_info_upgrade_test.go`
- ClusterMeta 测试示例：`tdsql/mc/server/cluster/meta_test.go`
- Go 官方测试文档：https://golang.org/pkg/testing/
<<<<<<< HEAD
- testify 文档：https://github.com/stretchr/testify
=======
- testify 文档：https://github.com/stretchr/testify
>>>>>>> 5a40ecd84ba (mc/logservice: support partition-level data locality constraints)
