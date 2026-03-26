# RocksDB 单元测试规范

> 适用于 `storage/tdstore/rocksdb/` 目录下的 RocksDB 内核代码测试。
> TDStore 业务代码测试请参考 `tdstore-unit-test.md`。

---

## 测试目录结构

RocksDB 测试文件分散在各模块目录中：

```
storage/tdstore/rocksdb/
├── db/*_test.cc              # 数据库核心测试（最多）
│   ├── td_db_basic_test.cc   # TDStore 特定测试
│   ├── td_db_compaction_test.cc
│   └── ...
├── cache/*_test.cc           # 缓存测试
├── env/*_test.cc             # 环境抽象测试
├── file/*_test.cc            # 文件系统测试
├── memtable/*_test.cc        # Memtable 测试
├── table/*_test.cc           # 表格式测试
├── utilities/*_test.cc       # 工具类测试
└── test_util/                # 测试工具
    ├── testharness.h         # 核心宏（ASSERT_OK 等）
    ├── testutil.h            # 测试工具类
    └── mock_time_env.h       # Mock 环境
```

---

## 文件命名规范

| 类型 | 格式 | 示例 |
|------|------|------|
| 通用测试 | `<feature>_test.cc` | `db_basic_test.cc`, `cache_test.cc` |
| TDStore 特定 | `td_<feature>_test.cc` | `td_db_basic_test.cc`, `td_db_compaction_test.cc` |
| 测试类 | `<Feature>Test` 或 `TD<Feature>Test` | `DBTest`, `TDDBBasicTest` |

---

## 测试框架

使用 GoogleTest 1.8.1：

```cpp
#include <gtest/gtest.h>
#include <gmock/gmock.h>
```

---

## 核心基类：DBTestBase

大多数 RocksDB 测试继承自 `DBTestBase`：

```cpp
#include "db/db_test_util.h"

class TDDBBasicTest : public DBTestBase {
 public:
  TDDBBasicTest() : DBTestBase("td_db_basic_test", /*env_do_fsync=*/false) {}
};
```

### DBTestBase 提供的功能

| 功能 | 说明 |
|------|------|
| `db_` | 测试用 DB 实例 |
| `env_` | `SpecialEnv` 实例，支持故障注入 |
| `dbname_` | 测试数据库路径 |
| `Put(key, value)` | 简化的写入方法 |
| `Get(key)` | 简化的读取方法 |
| `Flush()` | 刷盘 |
| `Close()` / `Reopen()` | 关闭/重开数据库 |

---

## 断言宏

### 核心宏（来自 `testharness.h`）

```cpp
// Status 断言
#define ASSERT_OK(s)  // Status 必须是 OK，否则测试失败
#define EXPECT_OK(s)  // Status 期望是 OK
#define ASSERT_NOK(s) // Status 必须不是 OK
#define EXPECT_NOK(s) // Status 期望不是 OK

// 正则匹配
#define ASSERT_MATCHES_REGEX(str, pattern)
#define EXPECT_MATCHES_REGEX(str, pattern)
```

### 使用规范

```cpp
// ✅ 正确：使用专用宏
ASSERT_OK(db->Put(WriteOptions(), "key", "value"));
EXPECT_NOK(db->Get(ReadOptions(), "nonexistent", &value));

// ❌ 错误：手动检查
Status s = db->Put(...);
ASSERT_TRUE(s.ok());  // 错误信息不够清晰
```

---

## 故障注入：SpecialEnv

`SpecialEnv` 允许模拟各种 I/O 故障：

```cpp
class SpecialEnv : public EnvWrapper {
 public:
  std::atomic<bool> no_space_{false};           // 磁盘满
  std::atomic<bool> drop_writes_{false};        // 丢弃写入
  std::atomic<bool> manifest_write_error_{false}; // MANIFEST 写入失败
  std::atomic<int> random_file_open_counter_{0};  // 文件打开计数
};
```

### 使用示例

```cpp
TEST_F(DBTest, NoSpaceError) {
  // 模拟磁盘满
  env_->no_space_.store(true);
  Status s = Put("key", "value");
  EXPECT_NOK(s);
  
  // 恢复正常
  env_->no_space_.store(false);
  ASSERT_OK(Put("key", "value"));
}

TEST_F(DBTest, WriteFailure) {
  // 模拟写入失败
  env_->drop_writes_.store(true);
  Status s = Flush();
  EXPECT_NOK(s);
}
```

---

## 参数化测试

适用于测试多种配置组合：

```cpp
class DBTestWithParam
    : public DBTest,
      public testing::WithParamInterface<std::tuple<uint32_t, bool>> {
 public:
  DBTestWithParam() {
    max_subcompactions_ = std::get<0>(GetParam());
    exclusive_manual_compaction_ = std::get<1>(GetParam());
  }

 protected:
  uint32_t max_subcompactions_;
  bool exclusive_manual_compaction_;
};

INSTANTIATE_TEST_CASE_P(
    DBTestWithParam, DBTestWithParam,
    ::testing::Combine(
        ::testing::Values(1, 4),   // max_subcompactions
        ::testing::Bool()          // exclusive_manual_compaction
    ));

TEST_P(DBTestWithParam, ManualCompaction) {
  // 测试会针对所有参数组合运行
}
```

---

## 运行测试

### 运行单个测试文件

```bash
./td_db_basic_test
```

### 过滤特定测试

```bash
./td_db_basic_test --gtest_filter="*GetRangeStatInfo*"
```

### 使用 CTest

```bash
ctest -R rocksdb_
```

---

## CMake 配置

```cmake
add_executable(td_my_feature_test td_my_feature_test.cc)
target_link_libraries(td_my_feature_test
    ${ROCKSDB_TESTUTILLIB}
    gtest
    gmock
    ${TDSTORE_SHARED_LIB}
)
add_test(NAME rocksdb_td_my_feature_test COMMAND td_my_feature_test)
```

---

## 必要的头文件

```cpp
#include "db/db_test_util.h"       // DBTestBase, SpecialEnv
#include "test_util/testharness.h" // ASSERT_OK, EXPECT_OK
#include "test_util/testutil.h"    // 测试工具类
```

---

## 最佳实践

### 1. 禁用 fsync

单元测试应禁用 fsync 以加快速度：

```cpp
// ✅ 正确
TDDBBasicTest() : DBTestBase("test_name", /*env_do_fsync=*/false) {}

// ❌ 错误：默认启用 fsync
TDDBBasicTest() : DBTestBase("test_name") {}
```

### 2. 使用 SpecialEnv 测试错误路径

不要跳过错误处理的测试：

```cpp
TEST_F(DBTest, HandleDiskFull) {
  env_->no_space_.store(true);
  Status s = Put("key", "value");
  EXPECT_TRUE(s.IsIOError());
}
```

### 3. 清理测试数据

`DBTestBase` 会自动清理，但如果手动创建目录需要手动清理：

```cpp
void TearDown() override {
  DBTestBase::TearDown();
  // 清理额外创建的目录
  DeleteDir(extra_dir_);
}
```

### 4. 测试 Reopen 场景

很多 bug 只在重启后暴露：

```cpp
TEST_F(DBTest, RecoveryAfterCrash) {
  ASSERT_OK(Put("key", "value"));
  ASSERT_OK(Flush());
  
  // 模拟重启
  Close();
  Reopen(CurrentOptions());
  
  // 验证数据持久化
  EXPECT_EQ("value", Get("key"));
}
```

---

## TDStore 特定测试

修改 RocksDB 内核时，如果是 TDStore 特定功能，测试文件应使用 `td_` 前缀：

```cpp
// storage/tdstore/rocksdb/db/td_my_feature_test.cc

#include "db/db_test_util.h"
#include "test_util/testharness.h"

namespace ROCKSDB_NAMESPACE {

class TDMyFeatureTest : public DBTestBase {
 public:
  TDMyFeatureTest() : DBTestBase("td_my_feature_test", /*env_do_fsync=*/false) {}
};

TEST_F(TDMyFeatureTest, BasicOperation) {
  // TDStore 特定功能测试
}

}  // namespace ROCKSDB_NAMESPACE

int main(int argc, char** argv) {
  ROCKSDB_NAMESPACE::port::InstallStackTraceHandler();
  ::testing::InitGoogleTest(&argc, argv);
  return RUN_ALL_TESTS();
}
```

---

## 新测试文件模板

```cpp
// storage/tdstore/rocksdb/db/td_<feature>_test.cc

#include "db/db_test_util.h"
#include "test_util/testharness.h"

namespace ROCKSDB_NAMESPACE {

class TDMyFeatureTest : public DBTestBase {
 public:
  TDMyFeatureTest() : DBTestBase("td_my_feature_test", /*env_do_fsync=*/false) {}
};

TEST_F(TDMyFeatureTest, BasicOperation) {
  // Arrange（准备）
  ASSERT_OK(Put("key", "value"));

  // Act（执行）
  std::string result;
  ASSERT_OK(db_->Get(ReadOptions(), "key", &result));

  // Assert（断言）
  EXPECT_EQ("value", result);
}

TEST_F(TDMyFeatureTest, ErrorPath) {
  // 使用 SpecialEnv 故障注入
  env_->drop_writes_.store(true);
  Status s = Put("key", "value");
  EXPECT_NOK(s);
}

TEST_F(TDMyFeatureTest, RecoveryAfterReopen) {
  ASSERT_OK(Put("key", "value"));
  ASSERT_OK(Flush());
  
  Close();
  Reopen(CurrentOptions());
  
  EXPECT_EQ("value", Get("key"));
}

}  // namespace ROCKSDB_NAMESPACE

int main(int argc, char** argv) {
  ROCKSDB_NAMESPACE::port::InstallStackTraceHandler();
  ::testing::InitGoogleTest(&argc, argv);
  return RUN_ALL_TESTS();
}
```
