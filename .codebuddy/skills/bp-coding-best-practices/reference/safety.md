# 安全性详细规则

## 命名空间

### 禁止 using namespace（尤其是头文件）

```cpp
// ❌ 污染命名空间
using namespace std;

// ✅ 显式限定
std::vector<int> v;
```

### 使用命名空间避免冲突

```cpp
namespace my_project::storage {

class Writer { /* ... */ };

}  // namespace my_project::storage
```

---

## Const 正确性

不修改的变量/参数标记 `const`：

```cpp
int sum(const std::vector<int>& xs) {
  int total = 0;
  for (const int x : xs) total += x;
  return total;
}
```

---

## 资源管理（跨语言原则）

- 使用作用域或托管对象绑定资源生命周期。
- 禁止把资源释放责任分散到多条分支路径。
- 新增 `return` / `early exit` 时，必须检查资源契约是否闭环（释放/回调/提交等）。

C++ 详细规则（`unique_ptr/shared_ptr/weak_ptr`、Rule of Zero/Five、dangling 场景）统一见：
- [std-company-cpp/reference/ownership-lifetime.md](../../std-company-cpp/reference/ownership-lifetime.md)

---

## 类型安全

### 避免宏，用 constexpr

```cpp
// ❌ 宏（绕过类型检查）
#define KB(x) ((x) * 1024)

// ✅ constexpr（类型安全）
constexpr size_t KB(size_t x) { return x * 1024; }
```

### 使用 enum class

```cpp
// ❌ 传统 enum（隐式转换、名字污染）
enum Mode { ReadOnly, ReadWrite };

// ✅ enum class
enum class Mode { ReadOnly, ReadWrite };

void open_db(Mode m);
open_db(Mode::ReadOnly);
```

### 避免隐式窄化

```cpp
// ❌ 隐式窄化（可能静默截断）
int x = 1'000'000'000;
short y = x;

// ✅ 显式转换
short y = static_cast<short>(x);

// ✅ 花括号初始化（编译期报错）
// short y{x};  // error: narrowing conversion
```

---

## 整数安全

### 溢出检查

```cpp
#include <limits>
#include <optional>

std::optional<int> safe_add(int a, int b) {
  if ((b > 0 && a > std::numeric_limits<int>::max() - b) ||
      (b < 0 && a < std::numeric_limits<int>::min() - b)) {
    return std::nullopt;  // overflow would occur
  }
  return a + b;
}
```

### 边界安全访问

```cpp
#include <vector>
#include <optional>

std::optional<int> get_if_in_range(const std::vector<int>& v, size_t i) {
  if (i >= v.size()) return std::nullopt;
  return v[i];
}
```

---

## 编译器辅助

### [[nodiscard]]

防止忽略重要返回值：

```cpp
[[nodiscard]] std::optional<int> parse_int(std::string_view s);

// 调用时如果忽略返回值，编译器警告
```

### override

防止签名不匹配：

```cpp
struct Base {
  virtual ~Base() = default;
  virtual void f() = 0;
};

struct Derived : Base {
  void f() override {}  // 如果签名不匹配，编译报错
};
```

---

## 错误处理

### 避免哨兵值

```cpp
// ❌ 哨兵值（-1 可能被误解）
int find_pos(const std::string& s, char c) {
  auto pos = s.find(c);
  return pos == std::string::npos ? -1 : pos;
}

// ✅ std::optional
std::optional<size_t> find_pos(std::string_view s, char c) {
  const auto pos = s.find(c);
  if (pos == std::string_view::npos) return std::nullopt;
  return pos;
}
```

### 断言

用于程序员错误（invariant），不用于用户输入校验：

```cpp
#include <cassert>

int at_least_one(int x) {
  assert(x >= 1);  // debug build invariant
  return x;
}
```

---

## 头文件规范

### Include Guard

```cpp
#ifndef MYPROJECT_UTIL_FOO_H_
#define MYPROJECT_UTIL_FOO_H_

#include <string>
#include <vector>

#include "myproject/util/bar.h"

#endif  // MYPROJECT_UTIL_FOO_H_
```

### Include 顺序

1. 对应的头文件（如 `foo.cc` 先 include `foo.h`）
2. 标准库
3. 第三方库
4. 项目内头文件
