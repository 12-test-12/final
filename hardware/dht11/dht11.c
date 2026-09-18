#include "dht11.h"

#define DHT11_RESPONSE_TIMEOUT_US  150U
#define DHT11_BIT_SAMPLE_US         40U

static uint8_t DHT11_WaitWhileLevel(uint8_t level, uint16_t timeoutUs)
{
    while (GPIO_ReadInputDataBit(DHT11_GPIO_PORT, DHT11_GPIO_PIN) == level)
    {
        if (timeoutUs == 0U)
        {
            return 1U;
        }

        timeoutUs--;
        delay_us(1U);
    }

    return 0U;
}

static uint8_t DHT11_ReadBitChecked(uint8_t *bit)
{
    /* 等待上一个高电平结束，进入本位的 50 us 低电平。 */
    if (DHT11_WaitWhileLevel(1U, DHT11_RESPONSE_TIMEOUT_US) != 0U)
    {
        return 3U;
    }

    /* 等待本位的高电平开始。 */
    if (DHT11_WaitWhileLevel(0U, DHT11_RESPONSE_TIMEOUT_US) != 0U)
    {
        return 4U;
    }

    delay_us(DHT11_BIT_SAMPLE_US);
    *bit = (GPIO_ReadInputDataBit(DHT11_GPIO_PORT, DHT11_GPIO_PIN) != Bit_RESET) ? 1U : 0U;
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

void DHT11_Mode(uint8_t mode)
{
    GPIO_InitTypeDef gpio;

    gpio.GPIO_Pin = DHT11_GPIO_PIN;
    gpio.GPIO_Speed = GPIO_Speed_50MHz;

    if (mode == OUT)
    {
        /* 开漏输出避免主机与 DHT11 同时驱动总线。 */
        gpio.GPIO_Mode = GPIO_Mode_Out_OD;
    }
    else
    {
        /* 内部上拉作为保护，硬件仍建议使用 4.7k~10k 上拉。 */
        gpio.GPIO_Mode = GPIO_Mode_IPU;
        GPIO_SetBits(DHT11_GPIO_PORT, DHT11_GPIO_PIN);
    }

    GPIO_Init(DHT11_GPIO_PORT, &gpio);
}

void DHT11_Rst(void)
{
    DHT11_Mode(OUT);
    DHT11_Low;
    delay_ms(20U);

    /*
     * 立即切换到带上拉的输入来释放总线，然后等待 DHT11 响应。
     * 若在开漏输出状态下先延时，没有外部上拉时总线无法及时回到高电平。
     */
    DHT11_High;
    DHT11_Mode(IN);
    delay_us(30U);
}

uint8_t DHT11_Check(void)
{
    /* DHT11 应在 150 us 内拉低总线。 */
    if (DHT11_WaitWhileLevel(1U, DHT11_RESPONSE_TIMEOUT_US) != 0U)
    {
        return 1U;
    }

    /* 等待 80 us 响应低电平结束。 */
    if (DHT11_WaitWhileLevel(0U, DHT11_RESPONSE_TIMEOUT_US) != 0U)
    {
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
    return 0U;
}

uint8_t DHT11_Init(void)
{
    RCC_APB2PeriphClockCmd(DHT11_GPIO_CLK, ENABLE);
    DHT11_Mode(OUT);
    DHT11_High;
    return 0U;
}
