#include "dht11.h"

/* 握手与数据位的等待上限，单位 us。
 *
 * 器件在主机释放总线后 20~40 us 开始应答，所以同步阶段必须留出足够余量；
 * 数据阶段的低电平固定 50 us、高电平最宽 70 us，600 us 足够区分"这一位还没
 * 来"和"这一位永远不来了"（帧已经结束，数据线停在空闲高电平）。 */
#define DHT11_SYNC_TIMEOUT_US   2000U
#define DHT11_EDGE_TIMEOUT_US    600U

/* 判位门限。DHT11 用高电平长度编码：26~28 us 表示 0，70 us 表示 1。
 * 取两者中间，两侧各留 20 us 余量，因此即使轮询有几微秒的抖动也不会判错。 */
#define DHT11_ONE_BIT_MIN_US       48U

/* 兜底轮数。正常情况下超时由周期计数器判定；这个上限只在 DWT 周期计数器
 * 不可用（CYCCNT 不计数）时才会触发，作用是让函数一定返回而不是死等。 */
#define DHT11_WAIT_GUARD_LOOPS 200000U

/* Cortex-M3 的 DWT 周期计数器。这一版 core_cm3.h 没有导出 DWT 结构体，
 * 所以按地址访问。它以内核时钟计数（8 MHz 主频下分辨率 0.125 us），比
 * delay_us 的毫秒级函数调用开销精确得多，也不与 delay.c 的 SysTick 抢资源。 */
#define DHT11_DEMCR        (*(volatile uint32_t *)0xE000EDFCUL)
#define DHT11_DWT_CTRL     (*(volatile uint32_t *)0xE0001000UL)
#define DHT11_DWT_CYCCNT   (*(volatile uint32_t *)0xE0001004UL)
#define DHT11_DEMCR_TRCENA    (1UL << 24)
#define DHT11_DWT_CYCCNTENA   1UL

/*
 * 数据线方向的切换直接写 CRL，不调用 GPIO_Init。
 *
 * GPIO_Init 会按位遍历 8 个引脚逐个比对，在 8 MHz、未开优化下一次要 100 us
 * 以上；而这里的每次切换都必须在一微秒级完成 —— 释放总线后器件只等 20~40 us
 * 就开始应答，切换慢一点整段应答就过去了，同步点会落到第一个数据位上。
 *
 * 下面两个配置值是 CRL/CRH 中 4 位一组的编码：CNF 在高两位，MODE 在低两位。
 */
#define DHT11_CRL_SHIFT 20U          /* PA5 位于 CRL 的 bit[23:20] */
#define DHT11_CFG_IN_PULL 0x8U       /* CNF=10 输入带上下拉，方向由 ODR 决定 */
#define DHT11_CFG_OUT_OD 0x7U        /* CNF=01 开漏输出，MODE=11 50 MHz */
#define DHT11_CFG_OUT_PP 0x3U        /* CNF=00 推挽输出，MODE=11 50 MHz */

/* 释放时推挽拉高的保持时间。器件在释放后 20~40 us 才开始驱动总线，
 * 取 5 us 既足够把线拉稳，又远早于器件可能开始驱动总线的时刻。 */
#define DHT11_RELEASE_KICK_US 5U

/* 每微秒的周期数，由 dht_time_init 按实际主频设置。 */
static uint32_t dht_cycles_per_us = 8U;

_Static_assert(DHT11_GPIO_PIN == GPIO_Pin_5,
               "DHT11_CRL_SHIFT 是按 PA5 写死的，换引脚必须同时改这里的位偏移");

#if DHT11_TRACE_ENABLE
volatile uint32_t DhtTraceCycle[DHT11_TRACE_DEPTH];
volatile uint32_t DhtTraceLevel[DHT11_TRACE_DEPTH];
volatile uint32_t DhtTraceCount;

/* 记一笔：level 0/1 是 dht_wait_until 等到的电平，2 是一次等待超时，
 * 3 是真正的释放时刻（由 dht_pin_release 记）。释放时刻这一笔是定位同步问题
 * 的关键 —— 有了它才能算出"释放后多久器件才拉低"，而不是反推。 */
#define DHT11_TRACE_NOTE(level)                                                \
    do {                                                                       \
        if (DhtTraceCount < DHT11_TRACE_DEPTH) {                               \
            DhtTraceCycle[DhtTraceCount] = DHT11_DWT_CYCCNT;                   \
            DhtTraceLevel[DhtTraceCount] = (level);                            \
            DhtTraceCount++;                                                   \
        }                                                                      \
    } while (0)
#else
#define DHT11_TRACE_NOTE(level)                                                \
    do {                                                                       \
    } while (0)
#endif

static void dht_pin_set_mode(uint32_t config)
{
    uint32_t crl = DHT11_GPIO_PORT->CRL;

    crl &= ~(0xFUL << DHT11_CRL_SHIFT);
    crl |= (config << DHT11_CRL_SHIFT);
    DHT11_GPIO_PORT->CRL = crl;
}

/* 释放总线：先用推挽输出把线快速拉高，再切成带上拉的输入。
 *
 * 只用内部上拉释放时上升沿要几十微秒（上拉约 40 kΩ，总线还有电容）。这段
 * 时间里器件可能已经越过它自己的输入门限、判定"主机已释放"并开始应答，而
 * 主机还在等自己的门限被越过 —— 双方的释放时刻不一致；更糟的是器件应答时
 * 把线拉低，主机的等待高电平会一直不成立，直到应答低电平结束才认为"线是高
 * 的"，于是同步点落到应答之后，整帧错开一位（现场轨迹：先测到 85 us 的高，
 * 那其实是应答高电平）。
 *
 * 推挽拉高把上升压到一微秒级，两种门限越过的时间就一致了。保持时间见
 * DHT11_RELEASE_KICK_US，之后切成输入，器件可以照常把线拉低。 */
static void dht_pin_release(void)
{
    uint32_t until;
    uint32_t guard = DHT11_WAIT_GUARD_LOOPS;

    dht_pin_set_mode(DHT11_CFG_OUT_PP);
    DHT11_GPIO_PORT->BSRR = DHT11_GPIO_PIN;

    until = DHT11_DWT_CYCCNT + (DHT11_RELEASE_KICK_US * dht_cycles_per_us);
    while ((int32_t)(DHT11_DWT_CYCCNT - until) < 0)
    {
        if (guard == 0U)
        {
            break;
        }
        guard--;
    }

    dht_pin_set_mode(DHT11_CFG_IN_PULL);
    DHT11_GPIO_PORT->BSRR = DHT11_GPIO_PIN;
}

/* 开漏拉低，用于发起一次读取。开漏避免主机与 DHT11 同时驱动总线。 */
static void dht_pin_pull_low(void)
{
    dht_pin_set_mode(DHT11_CFG_OUT_OD);
    DHT11_GPIO_PORT->BRR = DHT11_GPIO_PIN;
}

/* 使能周期计数器，并按实际主频记录每微秒的周期数。 */
static void dht_time_init(void)
{
    RCC_ClocksTypeDef clocks;

    DHT11_DEMCR |= DHT11_DEMCR_TRCENA;
    DHT11_DWT_CYCCNT = 0U;
    DHT11_DWT_CTRL |= DHT11_DWT_CYCCNTENA;

    RCC_GetClocksFreq(&clocks);
    dht_cycles_per_us = clocks.SYSCLK_Frequency / 1000000U;
    if (dht_cycles_per_us == 0U)
    {
        dht_cycles_per_us = 1U;
    }
}

/* 等到数据线变成 `level` 为止，超时返回 1。
 *
 * 这是一个紧循环：每轮只有一次引脚读和一次周期计数器读，没有函数调用。
 * 这一点是判位正确的前提 —— 上一版在循环里调用 delay_us(1)，在 8 MHz、
 * 未开优化的情况下每轮要十几微秒，已经超过一个 0 位高电平的一半宽度，
 * 会整位漏读并最终跑出帧外，表现为"时序错误"。 */
static uint8_t dht_wait_until(uint8_t level, uint32_t timeoutUs)
{
    uint32_t deadline = DHT11_DWT_CYCCNT + (timeoutUs * dht_cycles_per_us);
    uint32_t guard = DHT11_WAIT_GUARD_LOOPS;

    while ((uint8_t)GPIO_ReadInputDataBit(DHT11_GPIO_PORT, DHT11_GPIO_PIN) != level)
    {
        if ((int32_t)(DHT11_DWT_CYCCNT - deadline) >= 0)
        {
            DHT11_TRACE_NOTE(2U);
            return 1U;
        }
        if (guard == 0U)
        {
            DHT11_TRACE_NOTE(2U);
            return 1U;
        }
        guard--;
    }

    DHT11_TRACE_NOTE(level);
    return 0U;
}

/* 读一位：先等这一位的低电平结束，再量高电平持续了多久，按宽度判 0/1。
 *
 * 用宽度判定而不是"固定延时 40 us 后采样"，是因为宽度的两侧余量很大
 * （26 us 与 70 us 相差 44 us），轮询抖动几个微秒不影响判定；而固定采样点
 * 只有 28~70 us 这段窗口，一旦采样延时因为主频或优化级别而变长就会误判。 */
static uint8_t DHT11_ReadBitChecked(uint8_t *bit)
{
    uint32_t highCycles;

    if (dht_wait_until(1U, DHT11_EDGE_TIMEOUT_US) != 0U)
    {
        /* 数据线一直没有回到高电平：帧已经提前结束，或器件停在低电平。 */
        return 4U;
    }

    highCycles = DHT11_DWT_CYCCNT;
    if (dht_wait_until(0U, DHT11_EDGE_TIMEOUT_US) != 0U)
    {
        /* 数据线一直是高电平：帧已经结束，数据线停在空闲状态。 */
        return 3U;
    }
    highCycles = DHT11_DWT_CYCCNT - highCycles;

    *bit = (highCycles >= (DHT11_ONE_BIT_MIN_US * dht_cycles_per_us)) ? 1U : 0U;
    return 0U;
}

static uint8_t DHT11_ReadByteChecked(uint8_t *data)
{
    uint8_t i;
    uint8_t bit;
    uint8_t value = 0U;

    for (i = 0U; i < 8U; i++)
    {
        uint8_t error;

        value <<= 1U;
        error = DHT11_ReadBitChecked(&bit);
        if (error != 0U)
        {
            return error;
        }
        value |= bit;
    }

    *data = value;
    return 0U;
}

/* 切换数据线方向：OUT 为开漏输出（电平由调用方用 DHT11_Low / DHT11_High 决定），
 * IN 为带上拉的输入。开漏避免主机与 DHT11 同时驱动总线。 */
void DHT11_Mode(uint8_t mode)
{
    if (mode == OUT)
    {
        dht_pin_set_mode(DHT11_CFG_OUT_OD);
    }
    else
    {
        /* 内部上拉作为保护，硬件仍建议使用 4.7k~10k 上拉。 */
        dht_pin_release();
    }
}

void DHT11_Rst(void)
{
    /*
     * 拉低 20 ms 后释放。
     *
     * 释放必须是一次寄存器写、且紧跟轮询：器件在释放后 20~40 us 就拉低 80 us
     * 作为应答，中间只要夹着一次慢切换（GPIO_Init 要 100 us 以上），应答就会
     * 整段过去，同步点落到第一个数据位上，整帧错开一位，最终读到第 40 位时跑出
     * 帧外并返回错误码 3（现场轨迹已确认）。dht_pin_release 只写 CRL 和 BSRR，
     * 开销在一微秒级。
     */
    dht_pin_pull_low();
    delay_ms(20U);
    dht_pin_release();
}

uint8_t DHT11_Check(void)
{

    /*
     * 释放时线已被推挽拉高，直接从"等应答低电平"开始：
     *
     * 这里不能再先等一次高电平。器件越过它自己的输入门限后会开始应答并把线
     * 拉低，若主机此时还在等自己的门限被越过，就会一直等到应答低电平结束才
     * 认为"线高了"，同步点随即落到应答之后（现场轨迹里先测到的 85 us 高电平
     * 就是这种情况）。释放已经把上升沿做快，双方的释放时刻一致，直接等低电平
     * 接到的就是应答本身。
     *
     * 应答是"拉低 80 us 再拉高 80 us"，两者都比任何数据脉冲宽（数据位低电平
     * 50 us、高电平最宽 70 us），所以等到应答高电平结束的时刻，正好就是第一个
     * 数据位低电平的起点，整帧由此对齐。
     */
    if (dht_wait_until(0U, DHT11_SYNC_TIMEOUT_US) != 0U)
    {
        /* 释放总线后器件始终没有拉低：无应答。 */
        return 1U;
    }

    if (dht_wait_until(1U, DHT11_SYNC_TIMEOUT_US) != 0U)
    {
        /* 应答低电平没有结束。 */
        return 2U;
    }

    if (dht_wait_until(0U, DHT11_SYNC_TIMEOUT_US) != 0U)
    {
        /* 应答高电平没有结束。 */
        return 2U;
    }

    return 0U;
}

uint8_t DHT11_Read_Bit(void)
{
    uint8_t bit = 0U;
    (void)DHT11_ReadBitChecked(&bit);
    return bit;
}

uint8_t DHT11_Read_Byte(void)
{
    uint8_t data = 0U;
    (void)DHT11_ReadByteChecked(&data);
    return data;
}

uint8_t DHT11_Read_Data(uint8_t *temp, uint8_t *humi)
{
    uint8_t buffer[5];
    uint8_t i;
    uint8_t checksum;
    uint8_t error;

    if ((temp == 0) || (humi == 0))
    {
        return 7U;
    }

#if DHT11_TRACE_ENABLE
    /* 只保留失败那一次的轨迹：成功的读取在返回前会把缓冲清空。 */
    DhtTraceCount = 0U;
#endif

    DHT11_Rst();
    error = DHT11_Check();
    if (error != 0U)
    {
        return error;
    }

    for (i = 0U; i < 5U; i++)
    {
        error = DHT11_ReadByteChecked(&buffer[i]);
        if (error != 0U)
        {
            return error;
        }
    }

    checksum = (uint8_t)(buffer[0] + buffer[1] + buffer[2] + buffer[3]);
    if (checksum != buffer[4])
    {
        return 5U;
    }

    /* DHT11 只使用整数字节，与 DHT22 的数据格式不同。 */
    if ((buffer[0] > 100U) || (buffer[2] > 60U))
    {
        return 6U;
    }

    *humi = buffer[0];
    *temp = buffer[2];
#if DHT11_TRACE_ENABLE
    DhtTraceCount = 0U;
#endif
    return 0U;
}

uint8_t DHT11_Init(void)
{
    RCC_APB2PeriphClockCmd(DHT11_GPIO_CLK, ENABLE);
    dht_time_init();
    /* 空闲时保持带上拉的输入：总线在两次读取之间始终有确定电平，
     * 而不是靠开漏悬空。 */
    dht_pin_release();
    return 0U;
}

#if DHT11_TRACE_ENABLE
void DHT11_TraceReset(void)
{
    DhtTraceCount = 0U;
}
#endif
