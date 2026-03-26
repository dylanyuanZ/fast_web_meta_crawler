# TDStore 单元测试规范

> 适用于 `storage/tdstore/tdstore/` 目录下的业务代码测试。
> RocksDB 内核测试请参考 `rocksdb-unit-test.md`。

---

## 测试目录结构

```
storage/tdstore/tdstore/unittest/
├── test_include/          # 基类和 mock 工具
│   ├── test_region_base.h
│   ├── test_transaction_base.h
│   ├── test_storage_service_base.h
│   └── test_trans_part_utils.h
├── test_base.h            # DataSet 辅助类
├── test_utils.h           # 端口工具、shell 命令
├── common/                # 通用工具测试
├── connection/            # 连接相关测试
├── region/                # Region 管理测试
├── storage/               # 存储服务测试
├── transaction/           # 事务测试
├── rlog/                  # Raft 日志测试
└── tso/                   # TSO 测试
```

---

## 文件命名规范

| 类型 | 格式 | 示例 |
|------|------|------|
| 测试文件 | `test_<feature>.cc` 或 `test_td_<feature>.cc` | `test_td_utils.cc`, `test_td_pessimistic_trans.cc` |
| 测试类 | `TestTD<Feature>` | `TestTDPessimisticTransaction` |
| 测试用例 | 描述性命名 | `SuccessSingleTrxTest`, `SplitTest` |

---

## 测试框架

使用 GoogleTest 1.8.1：

```cpp
#include <gtest/gtest.h>
#include <gmock/gmock.h>
```

---

## 基类和工具

### 常用基类

| 文件 | 用途 |
|------|------|
| `test_include/test_region_base.h` | Mock `TDIReplicationGroup` |
| `test_include/test_transaction_base.h` | Mock `TDTransactionBaseImpl` |
| `test_include/test_storage_service_base.h` | Mock `TDIStorageService` |
| `test_include/test_trans_part_utils.h` | 事务参与者测试工具 |
| `test_base.h` | `DataSet` 辅助类，DB options 设置 |

### 优先使用已有基类

```cpp
// ✅ 正确：继承已有基类
class TestMyFeature : public TestTDTransPartUtils, public ::testing::Test { };

// ❌ 错误：重复造轮子
class TestMyFeature : public ::testing::Test {
  MyOwnMockStorageService mock_;  // 不要自己写 mock
};
```

---

## Fixture 模式

```cpp
class TestTDPessimisticTransaction : public TestTDTransPartUtils,
                                      public ::testing::Test {
 public:
  // 整个测试类只执行一次
  static void SetUpTestSuite() {
    // 初始化共享资源
  }

  static void TearDownTestSuite() {
    // 清理共享资源
  }

  // 每个测试用例执行前后
  void SetUp() override {
    // 初始化测试数据
  }

  void TearDown() override {
    // 清理测试数据
  }
};

TEST_F(TestTDPessimisticTransaction, SuccessSingleTrxTest) {
  // 测试代码
}
```

---

## 断言宏

### TDStore 专用宏

```cpp
// 错误码断言
#define EXPECT_SUCCESS(statement) EXPECT_EQ(tdcomm::EC_OK, (statement))
#define ASSERT_SUCCESS(statement) ASSERT_EQ(tdcomm::EC_OK, (statement))

// Protobuf 比较
#define EXPECT_PB_EQ(pb1, pb2)
```

### RocksDB Status 断言（TDStore 也可用）

```cpp
// 来自 rocksdb/test_util/testharness.h
#define ASSERT_OK(s)  // Status 必须是 OK
#define EXPECT_OK(s)  // Status 期望是 OK
#define ASSERT_NOK(s) // Status 必须不是 OK
#define EXPECT_NOK(s) // Status 期望不是 OK
```

### 使用规范

```cpp
// ✅ 正确：使用专用宏
EXPECT_SUCCESS(region->Start(true, true));
ASSERT_OK(db->Put(WriteOptions(), "key", "value"));

// ❌ 错误：手动比较
EXPECT_EQ(0, region->Start(true, true));
Status s = db->Put(...);
ASSERT_TRUE(s.ok());
```

---

## Mock/Stub 模式

TDStore 使用**自定义 stub 实现**（不使用 GMock）：

### Mock ReplicationGroup

```cpp
class TestTDReplicationGroupBaseImpl : public TDIReplicationGroup {
 public:
  int Start(const bool need_elect, const bool is_recommend_leader) override {
    return tdcomm::EC_OK;
  }
  int Stop() override { return tdcomm::EC_OK; }
  // 其他方法返回默认值
};
```

### Mock Transaction

```cpp
class TestTDTransactionBaseImpl : public TDTransactionBaseImpl {
 public:
  Status TryLock(ColumnFamilyHandle* cfh, const Slice& key, bool read_only,
                 bool exclusive, const bool do_validate, ...) override {
    return Status();
  }
  Status Commit() override { return Status(); }
  Status Rollback() override { return Status(); }
};
```

### Mock StorageService

```cpp
class TestTDStorageServiceBaseImpl : public TDIStorageService {
 public:
  DBImpl* GetDataDBImpl() override { return db_impl_; }
  TDRlogDB* GetRlogDB() override { return rlog_db_; }
  // 其他方法返回 mock 值
};
```

---

## DataSet 辅助类

用于批量生成测试数据：

```cpp
struct DataSet {
  void write(rocksdb::DBImpl* db, rocksdb::ColumnFamilyHandle* cfh, ...);
  rocksdb::Slice key(int32_t i);
  rocksdb::Slice value(int32_t i);
};

// 使用示例
DataSet data = create_put_data_set(/*prefix=*/1, /*start=*/0, /*end=*/100);
write_data_set(db, cfh, /*prefix=*/1, /*start=*/0, /*end=*/100);
```

---

## 参数化测试

适用于需要测试多种参数组合的场景：

```cpp
class TestWithParam
    : public TestTDTransPartUtils,
      public testing::WithParamInterface<std::tuple<uint32_t, bool>> {
 public:
  TestWithParam() {
    param1_ = std::get<0>(GetParam());
    param2_ = std::get<1>(GetParam());
  }

 protected:
  uint32_t param1_;
  bool param2_;
};

INSTANTIATE_TEST_CASE_P(
    TestWithParam, TestWithParam,
    ::testing::Combine(
        ::testing::Values(1, 4),
        ::testing::Bool()
    ));

TEST_P(TestWithParam, SomeTest) {
  // 使用 param1_ 和 param2_
}
```

---

## 运行测试

### 运行单个测试文件

```bash
./test_td_pessimistic_trans
```

### 过滤特定测试用例

```bash
./test_td_pessimistic_trans --gtest_filter="*SuccessSingleTrxTest*"
```

### 运行所有 TDStore 测试

```bash
cd storage/tdstore/tdstore/unittest
./run_all_tests.sh
```

### 使用 CTest

```bash
ctest -R tdstore_
```

---

## CMake 配置

```cmake
# 在测试目录的 CMakeLists.txt 中
add_executable(test_my_feature test_my_feature.cc)
target_link_libraries(test_my_feature
    ${ROCKSDB_TESTUTILLIB}
    gtest
    gmock
    ${TDSTORE_SHARED_LIB}
)
add_test(NAME tdstore_test_my_feature COMMAND test_my_feature)
```

---

## 必要的头文件

```cpp
#include <gtest/gtest.h>
#include "test_base.h"                          // DataSet
#include "test_include/test_region_base.h"      // Mock ReplicationGroup
#include "test_include/test_transaction_base.h" // Mock Transaction
#include "test_include/test_storage_service_base.h" // Mock StorageService
```

---

## Debug 注入点：TD_DEBUG_EXECUTE_IF

TDStore 提供了 debug 注入机制，用于在测试中控制特定代码路径的执行。

### 头文件

```cpp
#include "tdstore/common/td_debug.h"
```

### 在生产代码中埋点

```cpp
// 只在 Debug 编译且 keyword 被启用时执行
TD_DEBUG_EXECUTE_IF("my_debug_point", {
  // 注入的代码，如：模拟失败、记录日志、修改状态
  return Status::Corruption("injected error");
});

// 等待某个信号（用于同步测试）
TD_DEBUG_WAIT_IF("my_wait_point");
```

### 在测试中启用 debug 点

```cpp
#include "tdstore/common/td_debug.h"

TEST_F(TestMyFeature, InjectError) {
  std::string err_msg;
  
  // 启用 debug 点（+0 表示可重复执行）
  ASSERT_TRUE(DebugSet("+0,my_debug_point", err_msg));
  
  // 执行会触发注入代码的操作
  Status s = DoSomething();
  EXPECT_TRUE(s.IsCorruption());
  
  // 禁用 debug 点
  ASSERT_TRUE(DebugSet("-0,my_debug_point", err_msg));
}

TEST_F(TestMyFeature, ExecuteOnce) {
  std::string err_msg;
  
  // +1 表示只执行一次
  ASSERT_TRUE(DebugSet("+1,one_time_point", err_msg));
  
  int counter = 0;
  TD_DEBUG_EXECUTE_IF("one_time_point", { counter++; });
  TD_DEBUG_EXECUTE_IF("one_time_point", { counter++; });
  
  EXPECT_EQ(counter, 1);  // 只执行了一次
}
```

### 通过启动参数设置

```cpp
// 启动时设置初始 debug keywords
FLAGS_init_debug_keywords = "+0,foo1;+1,foo2";
InitDebugSet();
```

### 控制语法

| 语法 | 说明 |
|------|------|
| `+0,keyword` | 添加 keyword，可重复执行 |
| `+1,keyword` | 添加 keyword，只执行一次 |
| `-0,keyword` | 移除 keyword |
| `=keyword1,keyword2` | 重置为指定 keywords |
| `~keyword` | 发送信号（配合 `TD_DEBUG_WAIT_IF`） |

### 注意事项

- **仅在 Debug 编译时生效**（`#ifndef NDEBUG`）
- Release 编译时 `TD_DEBUG_EXECUTE_IF` 会被编译为空
- 不要在生产环境依赖这个机制

---

## 最佳实践

### 1. 测试数据清理

```cpp
void TearDown() override {
  DestroyDB(dbname_, Options());
  DeleteDir(test_dir_);
}
```

### 2. 避免真实 I/O

单元测试应尽量避免真实磁盘操作：

```cpp
// ✅ 正确：禁用 fsync
DBTestBase("test_name", /*env_do_fsync=*/false)

// ❌ 错误：使用真实文件系统
Options opts;
opts.env = Env::Default();
```

### 3. 测试用例独立

每个测试用例应该能独立运行，不依赖其他用例的执行顺序。

### 4. 测试边界条件

参考 `SKILL.md` 中的边界条件 checklist。

---

## 新测试文件模板

```cpp
// storage/tdstore/tdstore/unittest/<module>/test_<feature>.cc

#include <gtest/gtest.h>
#include "test_base.h"
#include "test_include/test_storage_service_base.h"

namespace tdstore {
namespace test {

class TestMyFeature : public ::testing::Test {
 public:
  static void SetUpTestSuite() {
    // 一次性初始化
  }

  static void TearDownTestSuite() {
    // 一次性清理
  }

  void SetUp() override {
    // 每个用例前
  }

  void TearDown() override {
    // 每个用例后
  }

 protected:
  // 测试 fixture
};

TEST_F(TestMyFeature, BasicFunctionality) {
  // Arrange（准备）

  // Act（执行）

  // Assert（断言）
}

TEST_F(TestMyFeature, ErrorHandling) {
  // 测试错误路径
}

}  // namespace test
}  // namespace tdstore

int main(int argc, char** argv) {
  ::testing::InitGoogleTest(&argc, argv);
  return RUN_ALL_TESTS();
}
```
